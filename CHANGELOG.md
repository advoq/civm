# Changelog

## [1.8.2](https://github.com/advoq/civm/compare/v1.8.1...v1.8.2) (2026-05-28)


### Bug Fixes

* **hook:** make job-started apt/journal/fstrim cleanup best-effort ([#55](https://github.com/advoq/civm/issues/55)) ([f9c9add](https://github.com/advoq/civm/commit/f9c9addb2965e14105c5c6007eed2b811ed64f51))
* **hook:** make job-started cleanup safe for concurrent jobs on shared runner ([#53](https://github.com/advoq/civm/issues/53)) ([6ec6b69](https://github.com/advoq/civm/commit/6ec6b6986cdad4f1b5025083dfd6eef2038ecdea))
* **hook:** prune dangling images only, never -a, on shared runner ([#56](https://github.com/advoq/civm/issues/56)) ([36d26b1](https://github.com/advoq/civm/commit/36d26b1cfc9875cc1bfe1035ab2e6dd528b13ab2))

## [1.8.1](https://github.com/advoq/civm/compare/v1.8.0...v1.8.1) (2026-05-25)


### Bug Fixes

* **templates:** limit Advoq router vet to local modules ([#51](https://github.com/advoq/civm/issues/51)) ([c454dd3](https://github.com/advoq/civm/commit/c454dd36389c2b10dd6667b17e1054fc0942c5a8))

## [1.8.0](https://github.com/advoq/civm/compare/v1.7.0...v1.8.0) (2026-05-23)


### Features

* **actions-metrics:** aggregate billable minutes + run counts cross-repo ([#49](https://github.com/advoq/civm/issues/49)) ([e39001f](https://github.com/advoq/civm/commit/e39001f2c6a460e579575cecce6ed7556c78509f))

## [1.7.0](https://github.com/advoq/civm/compare/v1.6.0...v1.7.0) (2026-05-23)


### Features

* **active-runs:** aggregate in_progress + queued runs cross-repo with ETA ([#47](https://github.com/advoq/civm/issues/47)) ([86e8dd4](https://github.com/advoq/civm/commit/86e8dd43c782bbdc5b74044d2076b5987db1ba74))

## [1.6.0](https://github.com/advoq/civm/compare/v1.5.1...v1.6.0) (2026-05-21)


### Features

* **hook:** graduated cache cleanup with per-cache caps + warn-tolerant routine ([#45](https://github.com/advoq/civm/issues/45)) ([cb8f3be](https://github.com/advoq/civm/commit/cb8f3be1d593716124dbfded997a0d258704d71d))

## [1.5.1](https://github.com/advoq/civm/compare/v1.5.0...v1.5.1) (2026-05-21)


### Bug Fixes

* parse cleanup journal RFC3339 offsets ([70a8e96](https://github.com/advoq/civm/commit/70a8e96bd7be81124ebecc62ae949d506e07007d))

## [1.5.0](https://github.com/advoq/civm/compare/v1.4.0...v1.5.0) (2026-05-21)


### Features

* generalize CI hooks and doctor checks ([4d47cf6](https://github.com/advoq/civm/commit/4d47cf6a2e9542524c4b8319de67a316b9070f17))
* harden runner and disk monitoring ([#41](https://github.com/advoq/civm/issues/41)) ([726d002](https://github.com/advoq/civm/commit/726d002481f9d167fdc561c721fa9b7b5a7755ce))


### Bug Fixes

* generate runner hook scripts ([fd87290](https://github.com/advoq/civm/commit/fd8729044336a8a1a6cc87d015b549ee184e3c2d))
* load runner watchdog gh auth env ([fd644ff](https://github.com/advoq/civm/commit/fd644ff2e8f4b43b8294d9f90f850167d22d2ddb))
* use app token for release automation ([e8fdcec](https://github.com/advoq/civm/commit/e8fdceca67d1e485fc068bbb84b466051cfdfb65))
* use valid runner hook script paths ([8857c20](https://github.com/advoq/civm/commit/8857c2004e105bd3ca210f00b9c6a7dcd073a9ce))

## [1.4.0](https://github.com/advoq/civm/compare/v1.3.0...v1.4.0) (2026-05-17)


### Features

* **civm:** consolidar status operacional dos peers ([#38](https://github.com/advoq/civm/issues/38)) ([f4b8ba3](https://github.com/advoq/civm/commit/f4b8ba32542e77249642d51e931ef5c7819da396))
* **civmctl:** add self-upgrade subcommand ([#33](https://github.com/advoq/civm/issues/33)) ([33fcbff](https://github.com/advoq/civm/commit/33fcbffb260c376a516c5bfbdda0dae8509fb5a1))


### Documentation

* **runner:** formalize self-upgrade deploy workflow ([#34](https://github.com/advoq/civm/issues/34)) ([c4083e8](https://github.com/advoq/civm/commit/c4083e8890615445a2dae6452c1b1dff000a6d0d))
* **security:** add SECURITY.md with threat model ([#36](https://github.com/advoq/civm/issues/36)) ([bc1d537](https://github.com/advoq/civm/commit/bc1d53756e1fe6e8943f30dc2c0a1400b7024661))

## [1.3.0](https://github.com/advoq/civm/compare/v1.2.0...v1.3.0) (2026-05-16)


### Features

* **metrics:** emit prometheus textfile for node_exporter ([#29](https://github.com/advoq/civm/issues/29)) ([d24611b](https://github.com/advoq/civm/commit/d24611b4db8264ad49a8d7121f66d1ac8504a811))


### Bug Fixes

* **hook:** preserve hot caches under \$HOME on job-completed ([#31](https://github.com/advoq/civm/issues/31)) ([de96cbf](https://github.com/advoq/civm/commit/de96cbf39a7de1159984ce55baa55fb950a97131))


### Refactor

* **hook:** use slog.JSONHandler for hook event log ([#28](https://github.com/advoq/civm/issues/28)) ([8246ab7](https://github.com/advoq/civm/commit/8246ab7717c43acdcc05a31c1783eb067a65aae0))

## [1.2.0](https://github.com/advoq/civm/compare/v1.1.3...v1.2.0) (2026-05-16)


### Features

* **ci:** add kahneman-sync audit across 14 peer repos ([e97476f](https://github.com/advoq/civm/commit/e97476ff045f0abd477068eee93aa63d4f685bf1))


### Refactor

* **hook:** eliminate shell wrappers via argv[0] dispatch ([#26](https://github.com/advoq/civm/issues/26)) ([1b8ecbe](https://github.com/advoq/civm/commit/1b8ecbefefb7715f70a108a87f9e5e689d2b9cd1))


### Documentation

* **governance:** align ssdv3 refs to disciplines/ path ([4375b7f](https://github.com/advoq/civm/commit/4375b7f3e7de2d3cb4354980b150deb188ae18a5))


### CI

* paid approval workflow + job hooks + capacity reporting ([#25](https://github.com/advoq/civm/issues/25)) ([0ace9e5](https://github.com/advoq/civm/commit/0ace9e52ac510a3f4926373129cdfe1494ebbaa9))

## [1.1.3](https://github.com/emersonbusson/civm/compare/v1.1.2...v1.1.3) (2026-05-11)


### Bug Fixes

* preserve release PR component parsing ([#17](https://github.com/emersonbusson/civm/issues/17)) ([5592580](https://github.com/emersonbusson/civm/commit/55925806a4b56ab52ccb77861e88f956f349f7be))
* remove release package component ([#18](https://github.com/emersonbusson/civm/issues/18)) ([1602578](https://github.com/emersonbusson/civm/commit/1602578e06b0b450b2a94bf6f5f7b44620655256))

## [1.1.2](https://github.com/emersonbusson/civm/compare/v1.1.1...v1.1.2) (2026-05-11)


### Bug Fixes

* set grouped release PR title ([#15](https://github.com/emersonbusson/civm/issues/15)) ([1e0cfb0](https://github.com/emersonbusson/civm/commit/1e0cfb0d340d9819186b3ba54d999012348e9377))

## [1.1.1](https://github.com/emersonbusson/civm/compare/v1.1.0...v1.1.1) (2026-05-11)


### Bug Fixes

* address release-please follow-ups ([#11](https://github.com/emersonbusson/civm/issues/11)) ([25544c6](https://github.com/emersonbusson/civm/commit/25544c65675a7834b8a7acdd9a8b4e4667c67e05))

## [1.1.0](https://github.com/emersonbusson/civm/compare/v1.0.0...v1.1.0) (2026-05-11)


### Features

* add release-please automation ([#8](https://github.com/emersonbusson/civm/issues/8)) ([a93789f](https://github.com/emersonbusson/civm/commit/a93789f3d6ad5789494703db31b79d145cf575df))


### Bug Fixes

* harden civmctl bootstrap and runner operations ([#6](https://github.com/emersonbusson/civm/issues/6)) ([4a1f590](https://github.com/emersonbusson/civm/commit/4a1f590ed832990d48901665beeadc9287c8acb1))
