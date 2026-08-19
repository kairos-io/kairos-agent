> ## This repository has been archived pending migration into kairos-io/kairos
>
> kairos-agent is being absorbed into the
> [kairos-io/kairos](https://github.com/kairos-io/kairos) monorepo as
> part of the plan tracked in
> [kairos-io/kairos#4301](https://github.com/kairos-io/kairos/issues/4301).
> Every existing tag remains resolvable via the Go module proxy and
> installable with the same
> `go get github.com/kairos-io/kairos-agent/v2@vX.Y.Z` as before, so
> anything already published keeps working.
>
> New development happens at
> [github.com/kairos-io/kairos/agent](https://github.com/kairos-io/kairos/tree/master/agent).
> If that link returns 404, the subtree import is not yet complete;
> see the tracking issue above for the current state and timeline.
>
> **To pick up newer kairos-agent code,** update your imports:
>
> ```
> find . -type f -name '*.go' -exec sed -i \
>   's|github.com/kairos-io/kairos-agent/v2|github.com/kairos-io/kairos/agent|g' {} +
> go mod tidy
> go get github.com/kairos-io/kairos@<latest>
> ```
>
> **Open PRs on this repository will not be merged here.** The
> branches remain reachable via `git fetch` from the archived repo, so
> nothing is lost. To carry unfinished work forward, fetch the branch,
> `git format-patch`, and `git am --directory=agent` against the
> monorepo, then apply the import-rewrite above.
>
> **On backports to older lines.** The default answer is: consume the
> latest release. We will only unarchive this repository for fixes we
> judge important enough (typically security fixes or breakage with no
> reasonable workaround). Convenience backports and feature backports
> do not qualify. To request one, open an issue on
> [kairos-io/kairos](https://github.com/kairos-io/kairos/issues)
> describing the kairos-agent version, the fix, and why moving to the
> latest release is not viable. If we agree the fix meets the bar, we
> will unarchive temporarily to land it and cut a new patch tag.
