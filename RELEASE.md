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
| [build/ios/Info.plist](build/ios/Info.plist) | `CFBundleShortVersionString` / `CFBundleVersion` | `0.1.2` / `0.1.2` | iOS bundle versions (numeric) |
| [build/ios/Info.dev.plist](build/ios/Info.dev.plist) | `CFBundleShortVersionString` / `CFBundleVersion` | `0.1.2-dev` / `0.1.2` | iOS dev bundle versions |
| [build/windows/info.json](build/windows/info.json) | `fixed.file_version` | `"0.1.2"` | Windows PE file version (numeric) |
| [build/windows/info.json](build/windows/info.json) | `info.0409.ProductVersion` | `"0.1.2-beta"` | Windows Explorer product version |
| [build/windows/wails.exe.manifest](build/windows/wails.exe.manifest) | `assemblyIdentity[@version]` | `"0.1.2"` | Windows SxS manifest (numeric) |
| [build/windows/nsis/wails_tools.nsh](build/windows/nsis/wails_tools.nsh) | `INFO_PRODUCTVERSION` | `"0.1.2"` | NSIS installer version (numeric) |
| [build/windows/msix/app_manifest.xml](build/windows/msix/app_manifest.xml) | `Identity[@Version]` | `"0.1.2.0"` | MSIX 4-part numeric version (`X.Y.Z.0`) |
| [build/windows/msix/template.xml](build/windows/msix/template.xml) | `PackageInformation[@Version]` | `"0.1.2.0"` | MSIX template 4-part numeric version |
| [CHANGELOG.md](CHANGELOG.md) | Release heading | `## [0.1.2-beta] - YYYY-MM-DD` | Release notes |

> **Warning:** Do not run `wails3 task common:update:build-assets` blindly without checking `git diff build/` afterward—Wails asset templates can overwrite custom fields in `build/darwin/`, `build/ios/`, `build/linux/`, and `build/windows/`.

---

## 2. Release Checklist

1. **Verify current version strings** (replace `0\.1\.2` with the outgoing version):
   ```bash
   grep -rn "0\.1\.2" --exclude-dir=node_modules --exclude-dir=dist --exclude-dir=.git .
   ```
2. **Update all version placeholders** listed in Section 1 to the target version (`X.Y.Z[-beta]`).
3. **Update `CHANGELOG.md`**: Move `[Unreleased]` entries under `## [X.Y.Z-beta] - YYYY-MM-DD`.
4. **Run full verification**:
   ```bash
   cd frontend && npm run check && npm run build && cd ..
   go build . ./internal/...
   go vet . ./internal/...
   go test -count=1 ./internal/...
   wails3 build
   ```
   Optionally cross-build Windows/macOS/Linux packages via `Taskfile.yml` / `wails3 package`.
5. **Commit & tag**:
   ```bash
   git add frontend/src/lib/store.svelte.ts build/ CHANGELOG.md
   git commit -m "chore(release): bump version to vX.Y.Z-beta"
   git tag -a vX.Y.Z-beta -m "Release vX.Y.Z-beta"
   ```
6. **Push commit and tag** (when ready to publish):
   ```bash
   git push origin master --follow-tags
   ```
