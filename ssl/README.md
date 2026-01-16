# SSL Certificates Directory

Place your SSL certificates in this directory. The application will recursively scan for certificate files.

## Certificate Matching

The application automatically matches certificate files (`.crt`) with their corresponding private key files (`.key`) based on the domain information contained within the SSL certificates, regardless of the file names used.

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
- The application reads the domain information from the certificate content itself and matches it with the corresponding key file
- The application will log an error if duplicate certificates are found for the same domain
