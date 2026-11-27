# Release & Versioning Guide

`wailsail` uses [Semantic Versioning](https://semver.org/) (`vMAJOR.MINOR.PATCH[-PRERELEASE]`, e.g. `v0.1.2-beta`).

---

## 1. Version Placeholders

Update every file below before tagging a release. Note that Windows manifests and Apple `CFBundleVersion` require numeric versions without pre-release suffixes (`X.Y.Z` or `X.Y.Z.0`).

| File | Field / Line | Format Example | Notes |
|---|---|---|---|
| [frontend/src/lib/store.svelte.ts](frontend/src/lib/store.svelte.ts) | `APP_VERSION` | `"v0.1.2 (beta)"` | Shown as a pill in `SettingsModal` |
| [build/config.yml](build/config.yml) | `info.version` | `"0.1.2-beta"` | Wails v3 project config |
| [build/linux/nfpm/nfpm.yaml](build/linux/nfpm/nfpm.yaml) | `version` | `"0.1.2-beta"` | Linux `.deb` / `.rpm` / `.apk` package version |
| [build/darwin/Info.plist](build/darwin/Info.plist) | `CFBundleShortVersionString` | `0.1.2-beta` | macOS display version |
| [build/darwin/Info.plist](build/darwin/Info.plist) | `CFBundleVersion` | `0.1.2` | macOS numeric build version (no suffix) |
| [build/darwin/Info.dev.plist](build/darwin/Info.dev.plist) | `CFBundleShortVersionString` / `CFBundleVersion` | `0.1.2-beta` / `0.1.2` | macOS dev bundle versions |
| [build/windows/info.json](build/windows/info.json) | `fixed.file_version` | `"0.1.2"` | Windows PE file version (numeric) |
| [build/windows/info.json](build/windows/info.json) | `info.0409.ProductVersion` | `"0.1.2-beta"` | Windows Explorer product version |
| [build/windows/wails.exe.manifest](build/windows/wails.exe.manifest) | `assemblyIdentity[@version]` | `"0.1.2"` | Windows SxS manifest (numeric) |
| [build/windows/nsis/wails_tools.nsh](build/windows/nsis/wails_tools.nsh) | `INFO_PRODUCTVERSION` | `"0.1.2"` | NSIS installer version (numeric) |
| [build/windows/msix/app_manifest.xml](build/windows/msix/app_manifest.xml) | `Identity[@Version]` | `"0.1.2.0"` | MSIX 4-part numeric version (`X.Y.Z.0`) |
| [build/windows/msix/template.xml](build/windows/msix/template.xml) | `PackageInformation[@Version]` | `"0.1.2.0"` | MSIX template 4-part numeric version |
| [CHANGELOG.md](CHANGELOG.md) | Release heading | `## [0.1.2-beta] - YYYY-MM-DD` | Release notes |

> **Warning:** Do not run `wails3 task common:update:build-assets` blindly without checking `git diff build/` afterward—Wails asset templates can overwrite custom fields in `build/darwin/`, `build/linux/`, and `build/windows/`.

---

## 2. Release Checklist

1. **Verify current version strings** (replace `0\.1\.2` with the outgoing version):
   ```bash
   grep -rn "0\.1\.2" --exclude-dir=node_modules --exclude-dir=dist --exclude-dir=.git .
   ```
2. **Update all version placeholders** listed in Section 1 to the target version (`X.Y.Z[-beta]`).
3. **Update `CHANGELOG.md`**: Move `[Unreleased]` entries under `## [X.Y.Z-beta] - YYYY-MM-DD`.
4. **Run validation, tests, build, and generate checksums via `scripts/build.sh`**:
   - **Local / single-host release build**:
     ```bash
     ./scripts/build.sh --version vX.Y.Z-beta
     ```
     This validates and tests the frontend (`check`, `test`, `build`) and Go backend (`vet`, `build`, `test -race`), builds the desktop binary, stages `bin/dist/wailsail-vX.Y.Z-beta-<os>-<arch>[.exe]`, and writes + verifies `bin/dist/SHA256SUMS`.
     Pass `--target linux --target windows` (and optional `--arch amd64`) to build multiple targets on one host.
   - **GitHub Actions 3-job release flow**:
     ```bash
     # Job 1 (test): fail fast on Ubuntu
     ./scripts/build.sh --test-only

     # Job 2 (build matrix): one job per OS/arch runner
     ./scripts/build.sh --skip-tests --target <os> --arch <arch> --version vX.Y.Z-beta

     # Job 3 (release): after downloading all matrix artifacts into dist/
     ./scripts/build.sh --checksums-only --dist-dir dist
     ```
5. **Commit & tag**:
   ```bash
   git add frontend/src/lib/store.svelte.ts build/ CHANGELOG.md
   git commit -m "chore(release): bump version to vX.Y.Z-beta"
   git tag -a vX.Y.Z-beta -m "Release vX.Y.Z-beta"
   ```
6. **Push commit, tag, and publish artifacts**:
   ```bash
   git push origin master --follow-tags
   ```
   Upload all staged binaries/packages and the combined `SHA256SUMS` file to the GitHub Release.
