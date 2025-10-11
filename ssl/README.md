# SSL Certificates Directory

Place your SSL certificates in this directory. The application will recursively scan for certificate files.

## Supported Certificate File Naming Patterns

The application supports the following naming patterns for SSL certificates:

1. `domain_bundle.crt` and `domain_bundle.key`

   - Example: `a.com_bundle.crt` and `a.com_bundle.key`

2. `domain.crt` and `domain.key`
   - Example: `a.com.crt` and `a.com.key`

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

- Each domain must have exactly one matching certificate (no duplicates allowed)
- Both `.crt` and `.key` files must exist for a certificate to be valid
- Certificate files are matched by domain name
- The application will log an error if duplicate certificates are found for the same domain
