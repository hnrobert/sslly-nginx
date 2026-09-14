package api

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	v1 "github.com/hnrobert/sslly-nginx/gen/hnrobert/sslly/v1"
	"github.com/hnrobert/sslly-nginx/internal/config"
	"github.com/hnrobert/sslly-nginx/internal/logger"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Deploy limits: bounded so a single request cannot fill the disk.
const (
	deployMaxBytes  = 200 << 20 // 200 MiB of decompressed content
	deployMaxFiles  = 20000
	deployTmpPrefix = ".deploy-tmp-"
	deployOldPrefix = ".deploy-old-"
)

type deployService struct {
	v1.UnimplementedDeployServiceServer
	srv *Server
}

// deployDir returns the absolute deploy root (creating it on demand). The
// default lives under the static volume so deployments survive container
// recreation without an extra mount.
func (s *Server) deployDir() (string, error) {
	root := s.cfg.DeployDir
	if root == "" {
		root = "./static/deploy"
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(abs, 0777); err != nil {
		return "", err
	}
	return abs, nil
}

// sanitizeDomainDir makes a filesystem-safe directory name for a domain,
// optionally with a /path suffix ("app.example.com/docs" ->
// "app.example.com__docs").
func sanitizeDomainDir(domain string) string {
	d := strings.ToLower(strings.TrimSpace(domain))
	d = strings.Trim(d, "/")
	d = strings.ReplaceAll(d, "/", "__")
	if d == "" {
		return "site"
	}
	return d
}

// validateDeployDomain accepts "example.com" or "example.com/path"; rejects
// protocols, ports, empty, and malformed hosts.
func validateDeployDomain(domain string) (string, string, error) {
	d := strings.TrimSpace(domain)
	if d == "" {
		return "", "", errors.New("domain is required")
	}
	if strings.ContainsAny(d, "<>|: ") {
		return "", "", fmt.Errorf("domain %q must be a plain domain with an optional /path", domain)
	}
	slash := strings.Index(d, "/")
	host, path := d, ""
	if slash >= 0 {
		host, path = d[:slash], d[slash:]
	}
	if host == "" || strings.Contains(host, "..") {
		return "", "", fmt.Errorf("invalid domain host %q", host)
	}
	for _, r := range host {
		ok := r == '.' || r == '-' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			return "", "", fmt.Errorf("invalid character %q in domain host", string(r))
		}
	}
	if path != "" && !strings.HasPrefix(path, "/") {
		return "", "", fmt.Errorf("path must start with /")
	}
	return host, path, nil
}

// unzipDist extracts a dist archive into dst with zip-slip protection,
// size/count limits, and single top-level wrapper stripping.
func unzipDist(distZip []byte, dst string) error {
	zr, err := zip.NewReader(bytes.NewReader(distZip), int64(len(distZip)))
	if err != nil {
		return fmt.Errorf("invalid zip archive: %v", err)
	}
	absDst, err := filepath.Abs(dst)
	if err != nil {
		return err
	}

	var totalFiles int
	var totalBytes int64
	for _, f := range zr.File {
		if totalFiles++; totalFiles > deployMaxFiles {
			return fmt.Errorf("archive has more than %d entries", deployMaxFiles)
		}
		name := filepath.Clean(f.Name)
		if strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			return fmt.Errorf("zip entry %q escapes the archive root", f.Name)
		}
		target := filepath.Join(absDst, name)
		if target != absDst && !strings.HasPrefix(target, absDst+string(filepath.Separator)) {
			return fmt.Errorf("zip entry %q escapes the destination", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0777); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0777); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
		if err != nil {
			rc.Close()
			return err
		}
		n, err := io.Copy(out, rc)
		out.Close()
		rc.Close()
		if err != nil {
			return err
		}
		if totalBytes += n; totalBytes > deployMaxBytes {
			return fmt.Errorf("archive decompresses beyond %d bytes", deployMaxBytes)
		}
	}

	// Strip a single wrapping directory (the usual "dist/" wrapper).
	stripWrapperDir(absDst)
	return nil
}

// stripWrapperDir lifts the contents up one level when dst contains exactly
// one directory and nothing else.
func stripWrapperDir(dst string) {
	entries, err := os.ReadDir(dst)
	if err != nil || len(entries) != 1 || !entries[0].IsDir() {
		return
	}
	wrapper := filepath.Join(dst, entries[0].Name())
	inner, err := os.ReadDir(wrapper)
	if err != nil {
		return
	}
	for _, e := range inner {
		if err := os.Rename(filepath.Join(wrapper, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return // leave as-is; the wrapper dir still serves correctly
		}
	}
	_ = os.Remove(wrapper)
}

func (d *deployService) DeployStatic(ctx context.Context, req *v1.DeployStaticRequest) (*v1.DeployStaticResponse, error) {
	if _, _, err := validateDeployDomain(req.GetDomain()); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	if len(req.GetDistZip()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "dist_zip is required")
	}
	if _, err := config.SplitGroupPath(req.GetGroup()); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	listenerKey := strings.ToLower(strings.TrimSpace(req.GetDomain()))
	if err := Authorize(userFromContext(ctx), config.SurfaceDeploy, config.ModeReadWrite,
		[]Resource{{Domain: domainOfListener(listenerKey), UpstreamKey: listenerKey, Group: req.GetGroup()}}); err != nil {
		return nil, err
	}

	root, err := d.srv.deployDir()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "deploy dir: %v", err)
	}
	staticKey := filepath.Join(root, sanitizeDomainDir(req.GetDomain()))

	// 1. Extract into a fresh temp dir beside the final location.
	tmp, err := os.MkdirTemp(root, deployTmpPrefix)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "stage dir: %v", err)
	}
	if err := unzipDist(req.GetDistZip(), tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	// 2. Atomic swap: old aside, new in place (rollback the swap on failure).
	final := staticKey
	var old string
	if _, err := os.Stat(final); err == nil {
		holder, merr := os.MkdirTemp(root, deployOldPrefix)
		if merr == nil {
			// Move the previous deployment INSIDE the fresh holder dir: a
			// rename target must not exist beforehand.
			if rerr := os.Rename(final, filepath.Join(holder, "prev")); rerr == nil {
				old = holder
			} else {
				merr = rerr
			}
		}
		if merr != nil {
			_ = os.RemoveAll(tmp)
			_ = os.RemoveAll(holder)
			return nil, status.Errorf(codes.Internal, "swap out previous deployment: %v", merr)
		}
	}
	if err := os.Rename(tmp, final); err != nil {
		if old != "" {
			_ = os.Rename(filepath.Join(old, "prev"), final) // put the previous files back
		}
		_ = os.RemoveAll(tmp)
		return nil, status.Errorf(codes.Internal, "activate deployment: %v", err)
	}

	// 3. Write the static route and run the validated reload pipeline. On
	// failure proxy.yaml is restored; the new files stay (the next deploy
	// overwrites them) — simple-overwrite semantics.
	prev, err := d.srv.cfg.Editor.SetProxyEntry(req.GetGroup(), staticKey, []string{listenerKey})
	if err != nil {
		// Editor rejected the write: restore the previous files.
		_ = os.RemoveAll(final)
		if old != "" {
			_ = os.Rename(filepath.Join(old, "prev"), final)
		}
		return nil, status.Errorf(codes.Internal, "write proxy.yaml: %v", err)
	}
	applyResult := d.srv.apply(config.ProxyConfigFile, prev)
	if old != "" {
		_ = os.RemoveAll(old)
	}
	if !applyResult.GetApplied() {
		logger.Warn("DeployStatic: reload rejected for %s: %s (files remain, route rolled back)", listenerKey, applyResult.GetError())
	}

	logger.Info("API deployed static site: %s -> %s (group=%q)", listenerKey, final, req.GetGroup())
	return &v1.DeployStaticResponse{
		StaticKey: staticKey,
		Apply:     applyResult,
	}, nil
}

func (d *deployService) DeleteStatic(ctx context.Context, req *v1.DeleteStaticRequest) (*v1.DeleteStaticResponse, error) {
	if _, _, err := validateDeployDomain(req.GetDomain()); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	if _, err := config.SplitGroupPath(req.GetGroup()); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	listenerKey := strings.ToLower(strings.TrimSpace(req.GetDomain()))
	if err := Authorize(userFromContext(ctx), config.SurfaceDeploy, config.ModeReadWrite,
		[]Resource{{Domain: domainOfListener(listenerKey), UpstreamKey: listenerKey, Group: req.GetGroup()}}); err != nil {
		return nil, err
	}

	root, err := d.srv.deployDir()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "deploy dir: %v", err)
	}
	staticKey := filepath.Join(root, sanitizeDomainDir(req.GetDomain()))

	prev, err := d.srv.cfg.Editor.DeleteProxyEntry(req.GetGroup(), staticKey)
	if err != nil {
		if errors.Is(err, config.ErrEntryNotFound) {
			return nil, status.Errorf(codes.NotFound, "no deployment for %q in group %q", req.GetDomain(), req.GetGroup())
		}
		return nil, status.Errorf(codes.Internal, "write proxy.yaml: %v", err)
	}
	applyResult := d.srv.apply(config.ProxyConfigFile, prev)
	if applyResult.GetApplied() {
		if err := os.RemoveAll(staticKey); err != nil {
			logger.Warn("DeleteStatic: route removed but files left at %s: %v", staticKey, err)
		}
	}
	logger.Info("API deleted static site: %s", listenerKey)
	return &v1.DeleteStaticResponse{Apply: applyResult}, nil
}
