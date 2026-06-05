# Storage Design

V1 starts with local filesystem storage and later adds S3-compatible storage.

## Interface goals

Storage should support:

- saving file blobs;
- reading file blobs;
- saving manifest JSON;
- reading manifest JSON;
- verifying SHA256;
- separating group data;
- avoiding path traversal.

## Content-addressed objects

File blobs should be stored by hash where possible:

```text
objects/sha256/<first-two>/<sha256>
```

This avoids duplicate file storage and makes integrity verification explicit.

## Snapshot metadata

Coordinator owns snapshot validity. Storage only stores bytes.

A snapshot becomes usable only after Coordinator marks it `available`.
