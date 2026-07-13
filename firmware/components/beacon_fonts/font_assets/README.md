# Local Font Assets

This directory intentionally contains no font files in source control. From the
repository root, install your own licensed copies before building:

```bash
./scripts/install-local-fonts.sh --source-dir /absolute/path/to/your/milan-fonts
```

The required filenames are `MiLanPro-Medium-400.ttf` and
`MiLanPro-SemiBold-540.ttf`. They are local-only and ignored by Git. The
project's MIT license does not cover them or grant font rights. Confirm that
your license permits device embedding, and also redistribution before sharing a
compiled firmware image that contains the fonts.
