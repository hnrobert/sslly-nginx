package api

import (
	"context"

	v1 "github.com/hnrobert/sslly-nginx/gen/hnrobert/sslly/v1"
	"github.com/hnrobert/sslly-nginx/internal/config"
	"github.com/hnrobert/sslly-nginx/internal/logger"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type usersService struct {
	v1.UnimplementedUsersServiceServer
	srv *Server
}

// lastAdminGuard rejects changes that would leave zero users with users-scope
// read-write access (prevents self-lockout). applied simulates the change on
// a copy of the current user list.
func lastAdminGuard(current []config.User, applied func([]config.User) []config.User) error {
	next := applied(append([]config.User(nil), current...))
	for i := range next {
		if hasUsersAdmin(next[i]) {
			return nil
		}
	}
	return status.Error(codes.FailedPrecondition,
		"refusing change that would remove the last users-scope read-write admin")
}

func (u *usersService) ListUsers(ctx context.Context, _ *v1.ListUsersRequest) (*v1.ListUsersResponse, error) {
	if err := Authorize(userFromContext(ctx), config.SurfaceUsers, config.ModeRead, nil); err != nil {
		return nil, err
	}
	users, err := u.srv.cfg.Users.Users()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "load users: %v", err)
	}
	resp := &v1.ListUsersResponse{}
	for _, usr := range users {
		resp.Users = append(resp.Users, userFromConfig(usr))
	}
	return resp, nil
}

func (u *usersService) UpsertUser(ctx context.Context, req *v1.UpsertUserRequest) (*v1.UpsertUserResponse, error) {
	newUser, err := userToProtoUser(req.GetUser())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	if req.GetToken() == "" && newUser.Name != userFromContext(ctx).Name {
		// Creating a brand-new user without a token would yield an unusable
		// (hash-less) account; keep-token is only meaningful for updates.
		users, err := u.srv.cfg.Users.Users()
		if err == nil {
			exists := false
			for _, x := range users {
				if x.Name == newUser.Name {
					exists = true
					break
				}
			}
			if !exists {
				return nil, status.Error(codes.InvalidArgument, "token is required when creating a new user")
			}
		}
	}

	if err := Authorize(userFromContext(ctx), config.SurfaceUsers, config.ModeReadWrite, nil); err != nil {
		return nil, err
	}

	users, err := u.srv.cfg.Users.Users()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "load users: %v", err)
	}
	if err := lastAdminGuard(users, func(list []config.User) []config.User {
		replaced := false
		for i := range list {
			if list[i].Name == newUser.Name {
				list[i] = newUser
				replaced = true
				break
			}
		}
		if !replaced {
			list = append(list, newUser)
		}
		return list
	}); err != nil {
		return nil, err
	}

	var hash string
	if req.GetToken() != "" {
		hash = config.HashToken(req.GetToken())
	}
	if _, err := u.srv.cfg.Editor.UpsertUser(newUser, hash); err != nil {
		return nil, status.Errorf(codes.Internal, "write users.yaml: %v", err)
	}
	logger.Info("API user upserted: %s", newUser.Name)

	return &v1.UpsertUserResponse{User: userFromConfig(newUser)}, nil
}

func (u *usersService) DeleteUser(ctx context.Context, req *v1.DeleteUserRequest) (*v1.DeleteUserResponse, error) {
	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if err := Authorize(userFromContext(ctx), config.SurfaceUsers, config.ModeReadWrite, nil); err != nil {
		return nil, err
	}

	users, err := u.srv.cfg.Users.Users()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "load users: %v", err)
	}
	found := false
	for _, x := range users {
		if x.Name == req.GetName() {
			found = true
			break
		}
	}
	if !found {
		return nil, status.Errorf(codes.NotFound, "user %q not found", req.GetName())
	}

	if err := lastAdminGuard(users, func(list []config.User) []config.User {
		out := list[:0]
		for _, x := range list {
			if x.Name != req.GetName() {
				out = append(out, x)
			}
		}
		return out
	}); err != nil {
		return nil, err
	}

	if _, err := u.srv.cfg.Editor.DeleteUser(req.GetName()); err != nil {
		return nil, status.Errorf(codes.Internal, "write users.yaml: %v", err)
	}
	logger.Info("API user deleted: %s", req.GetName())

	return &v1.DeleteUserResponse{}, nil
}
