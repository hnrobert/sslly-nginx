# SSL Certificates Directory

Place your SSL certificates in this directory. The application will recursively scan for certificate files.

## Certificate Matching

The application automatically matches certificate files (`.pem`/`.crt`) with their corresponding private key files (`.key`) based on the domain information contained within the SSL certificates, regardless of the file names used.

## Directory Structure

You can organize certificates in subdirectories. The application will recursively search through all subdirectories.

Example structure:

```tree
ssl/
├── a.com/
│   ├── a.com_bundle.crt
│   └── a.com.key
├── b.com.crt
├── b.com.key
└── subdomains/
    ├── b.a.com_bundle.crt
    └── b.a.com.key
```

## Important Notes

- Duplicate certificates are allowed. For each domain, only certificate+private-key pairs are considered valid; if multiple pairs match, the certificate with the farthest expiration time is selected (ties prefer `.pem` over `.crt`)
- Private key files are optional (if no matching key is found, the domain will be served over HTTP)
- The application reads the domain information from the certificate content itself and matches it with the corresponding key file
- The application logs warnings when duplicates are detected and when one certificate is preferred over another
