# Current repository and GitHub state

- `compose.yaml` already points five services at `ghcr.io/leonfox28/simplus-{control,agent,netd}` with a default `v0.1.0` tag.
- `.github/workflows/containers.yml` already builds the three targets and only pushes on `v*`, but its tag validation is broad and it publishes corresponding-source assets before the image matrix.
- `docs/installation.md` and README still assume a repository checkout for Compose/host scripts and document local image build as the pre-release fallback.
- `make container-build` is an intentional developer validation target and should remain outside the production installation path.
- The public GitHub repository had no tags or Releases when planning began; the containers workflow had only successful PR runs. Package listing was unavailable to the local token, and no tag-triggered publication had occurred.
- GitHub documents that a first Container registry package defaults to private; a public package permits anonymous pulls, and changing a personal package to public is an explicit owner action.
- Local `main` was fast-forwarded to `origin/main` before creating `feat/ghcr-release-install`; the only upstream container difference was the already-merged pinned Go base-image update.
- Existing specs require the three-image privilege split, source-before-image GPL publication, public/private evidence boundary, and no stable deployment claim before clean-VM lifecycle evidence.
