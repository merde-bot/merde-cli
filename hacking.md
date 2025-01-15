As stated in the README, we do not accept code contributions.

This file is intended for employees working on this codebase.

## Dev setup

The main additional item of note is the `merde config` command.

You can use to set an alternative server root, probably:

```sh
go run . config server http://localhost:8080
```

(TODO: use a different config file for release vs non-release versions.)

## Releasing

To make and publish a release using goreleaser:

```sh
export MERDE_RELEASE_TAG=<release tag like v0.0.99>
export PRIVATE_KEY_PATH=</path/to/home/.ssh/ssh_key_path>
export GITHUB_TOKEN=<goreleaser github token in 1password>
git tag -a $MERDE_RELEASE_TAG -m <some commit message>
git push origin $MERDE_RELEASE_TAG
goreleaser release --clean
```

* check https://github.com/merde-bot/merde-cli/releases
* check https://github.com/merde-bot/homebrew-tap
* `brew upgrade merde-bot/tap/merde && merde version`

TODO: automate some of this in a GitHub CI workflow on tag push.
