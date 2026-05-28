# Range-request spike (2026-05-28)

Outcome: A — huggingface_hub does NOT send `Range` headers on first downloads.

## How the spike was run

`huggingface-cli` has been deprecated in huggingface_hub v1.16.4 (the version
available in this environment). The replacement CLI (`hf`) was not installed
system-wide. Rather than relying on the CLI, we performed a static source-code
inspection of `huggingface_hub.file_download` (the module that implements all
HTTP downloads used by both the CLI and the Python API).

```
uv run --with huggingface_hub python3 -c "import inspect; ..."
```

## Evidence from source inspection

Key lines in `huggingface_hub/file_download.py` (v1.16.4):

```python
# L1842: resume_size is determined by current file position (0 on first download)
resume_size = f.tell()

# L362-363: Range header is ONLY set when resume_size > 0
if resume_size > 0:
    headers["Range"] = _adjust_range_header(headers.get("Range"), resume_size)
elif expected_size and expected_size > constants.MAX_HTTP_DOWNLOAD_SIZE:
    # Files > 50 GB raise ValueError (require hf_xet), never reach Range logic
    raise ValueError(...)

# L1871-1878: http_get called with resume_size from f.tell()
http_get(
    url_to_download,
    f,
    resume_size=resume_size,   # 0 on first download → no Range header
    headers=headers,
    ...
)
```

On a fresh download (`incomplete_path` does not exist or is empty),
`f.tell() == 0`, so the `if resume_size > 0` branch is never taken and
**no `Range` header is emitted**.

Range headers are only sent during resume after a previous partial download
(network interruption / retry).

## Implication for v1 resolver

The resolver in this package performs a plain GET to upstream on cache miss
and serves the full body. No special Range-stripping logic is needed for the
common download path.

Range header pass-through on client requests served from cache is fine; we
simply do not cache partial responses in v1 (see spec §7.6).

## If behaviour changes in the future

If a future version of huggingface_hub starts using Range headers by default
(e.g. for parallel chunked downloads), revisit:

- Option (a): strip the `Range` header on upstream calls and force a full
  download before caching.
- Option (b): add range-aware cache semantics (Content-Range, multipart
  assembly).

The integration tests in Phase 6 use a mock upstream and will catch
Range-related regressions regardless of live network behaviour.
