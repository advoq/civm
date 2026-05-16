# Changelog

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
