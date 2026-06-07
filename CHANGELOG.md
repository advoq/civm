# Changelog

## [1.18.1](https://github.com/advoq/civm/compare/v1.18.0...v1.18.1) (2026-06-07)


### Bug Fixes

* **runner:** self-cleaning runner — kill the V: disk death-spiral durably ([#115](https://github.com/advoq/civm/issues/115)) ([ff5d2b9](https://github.com/advoq/civm/commit/ff5d2b93fe223659eae442a11450f057dd108b18))

## [1.18.0](https://github.com/advoq/civm/compare/v1.17.2...v1.18.0) (2026-06-05)


### Features

* **admit:** memory-aware admission gate (civmctl admit) ([#112](https://github.com/advoq/civm/issues/112)) ([801319a](https://github.com/advoq/civm/commit/801319a2df52b7237ef09fdd246e42c2bc0f4a04))

## [1.17.2](https://github.com/advoq/civm/compare/v1.17.1...v1.17.2) (2026-06-05)


### Documentation

* **disciplines:** rewrite SSDV3-PROMPTS civm-native ([#110](https://github.com/advoq/civm/issues/110)) ([c5e6edb](https://github.com/advoq/civm/commit/c5e6edbb3b6f6fe447238a14e3a26048ae0ff2dc))

## [1.17.1](https://github.com/advoq/civm/compare/v1.17.0...v1.17.1) (2026-06-05)


### Documentation

* purge discontinued compexhub peer from runbooks/specs ([#109](https://github.com/advoq/civm/issues/109)) ([11e46bf](https://github.com/advoq/civm/commit/11e46bf7908ca3c1192c830d7c4be6ea9eac9b73))
* **rules:** purge web-app/compexhub content, make rules civm-native ([#107](https://github.com/advoq/civm/issues/107)) ([4e25e9e](https://github.com/advoq/civm/commit/4e25e9efd8c495c099f0df571ef8669c67c0e46c))

## [1.17.0](https://github.com/advoq/civm/compare/v1.16.0...v1.17.0) (2026-06-05)


### Features

* **reclaim:** instrument autoreclaim with scratch high-water poll ([#104](https://github.com/advoq/civm/issues/104)) ([4e7e51c](https://github.com/advoq/civm/commit/4e7e51cde7835877c6d16a38aac4178ab4443bbd))

## [1.16.0](https://github.com/advoq/civm/compare/v1.15.0...v1.16.0) (2026-06-05)


### Features

* **reclaim:** SPECv3 admission gate breaks host headroom deadlock ([#100](https://github.com/advoq/civm/issues/100)) ([25bb84c](https://github.com/advoq/civm/commit/25bb84c0caebece89961f7b751e9b7bf8d8631fb))


### Bug Fixes

* **optimize:** sudo the maintenance drain (root-owned lock) ([#103](https://github.com/advoq/civm/issues/103)) ([6d535be](https://github.com/advoq/civm/commit/6d535be3389dd05781305cd77c853a53304520db))

## [1.15.0](https://github.com/advoq/civm/compare/v1.14.2...v1.15.0) (2026-06-05)


### Features

* **memwatchdog:** monitoramento de RAM em tempo real ([d7c1b08](https://github.com/advoq/civm/commit/d7c1b08653b51bf517ae3ce6c04b38188746476e))
* **memwatchdog:** real-time RAM pressure monitoring ([ae52277](https://github.com/advoq/civm/commit/ae52277daa1b31f57cf1a02afc9ceece62f425c1))
* **templates:** cancel-on-pr-close workflow template ([e71bfa2](https://github.com/advoq/civm/commit/e71bfa288b053d1eb9de7ddb53a0c083726b13b0))
* **templates:** cancel-on-pr-close workflow template ([0fef0c4](https://github.com/advoq/civm/commit/0fef0c448ae3b84b08942db0bb0fb830561a8260))


### Bug Fixes

* **host:** vhdx watchdog honors both reclaim locks before start ([#98](https://github.com/advoq/civm/issues/98)) ([4f33111](https://github.com/advoq/civm/commit/4f331118c7c6ebd16738d9fce4e4dfc18f262144))


### Documentation

* **disciplines:** add architectural-noise audit superprompt ([#96](https://github.com/advoq/civm/issues/96)) ([78bcf75](https://github.com/advoq/civm/commit/78bcf758ff2377ff21fd7b76a825ef72237a3fc9))
* **spec:** SPECv3 host reclaim deadlock resilience ([#99](https://github.com/advoq/civm/issues/99)) ([1e882d1](https://github.com/advoq/civm/commit/1e882d1ffa6d5bae9134b0a5584a1c305fdec598))

## [1.14.2](https://github.com/advoq/civm/compare/v1.14.1...v1.14.2) (2026-06-04)


### Bug Fixes

* **reaper:** force-cancel para drenar runs presos em queued ([9155971](https://github.com/advoq/civm/commit/9155971ab422c455ff61c935dde75c1c9cb4109e))
* **reaper:** force-cancel to actually drain runs stuck queued ([193de84](https://github.com/advoq/civm/commit/193de84faabb57433992715aaf97e5a3b6fa6360))


### Documentation

* **disciplines:** rewrite as civm-native, drop compexhub templates ([fbea438](https://github.com/advoq/civm/commit/fbea4389c842410b34457d27d9d660b10ef49fa6))
* **rules:** add Go coding-style hygiene rule ([b7ac136](https://github.com/advoq/civm/commit/b7ac136bccbf3fe7550554f8171d5fda6a721ef0))
* **rules:** regra de coding-style (higiene de código Go) ([b986b33](https://github.com/advoq/civm/commit/b986b33e673ede2e2e365ee76f24f4086a8e6fbb))

## [1.14.1](https://github.com/advoq/civm/compare/v1.14.0...v1.14.1) (2026-06-04)


### Documentation

* **templates:** drop redundant self-contained boilerplate ([bb2dd93](https://github.com/advoq/civm/commit/bb2dd931ada80f5cadde9b4bb482a97f4bf9ff9f))
* **templates:** drop redundant self-contained boilerplate ([e89c366](https://github.com/advoq/civm/commit/e89c36661a2d975236ea96b42122570c9ad37815))

## [1.14.0](https://github.com/advoq/civm/compare/v1.13.1...v1.14.0) (2026-06-04)


### Features

* **reaper:** cancel queued/in_progress runs of closed PRs ([10d1d13](https://github.com/advoq/civm/commit/10d1d136dc83e0cb77e3027ecd8500fde88b62c3))
* **reaper:** civmctl reap-runs — cancela runs de PRs já fechados ([e72433a](https://github.com/advoq/civm/commit/e72433aa8ec86dde8c28379c56555cd858be85ca))

## [1.13.1](https://github.com/advoq/civm/compare/v1.13.0...v1.13.1) (2026-06-04)


### Bug Fixes

* **host:** clamp autoreclaim gap math with [int64]0 to stop V: fill ([1b70ddd](https://github.com/advoq/civm/commit/1b70ddde3b069e0d4dedd7e150e1b01e97dbfbc9))
* **host:** stop V: fill (autoreclaim Int32) + auto-restart wedged runner ([2b4ff33](https://github.com/advoq/civm/commit/2b4ff334601766adae6ec2ed9c5b17dd126612f6))

## [1.13.0](https://github.com/advoq/civm/compare/v1.12.0...v1.13.0) (2026-06-02)


### Features

* **runner:** auto-restart wedged runner via hooks.jsonl sentinel ([#79](https://github.com/advoq/civm/issues/79)) ([ce1ad5c](https://github.com/advoq/civm/commit/ce1ad5c5ad35db5adb4bdfc3d932503fc33c2b6b))

## [1.12.0](https://github.com/advoq/civm/compare/v1.11.3...v1.12.0) (2026-06-02)


### Features

* **hook:** log runner WorkRoot in hook record ([#78](https://github.com/advoq/civm/issues/78)) ([ec1286b](https://github.com/advoq/civm/commit/ec1286b25acdf168841908da8672468dcd2c31f4))


### Bug Fixes

* **cleanup:** treat safedelete refusal as non-fatal skip ([#76](https://github.com/advoq/civm/issues/76)) ([f65a5ac](https://github.com/advoq/civm/commit/f65a5ac40ca1cd595b2480e25c3ec8d961eeadef))

## [1.11.3](https://github.com/advoq/civm/compare/v1.11.2...v1.11.3) (2026-06-02)


### Bug Fixes

* **cleanup:** reclaim unused docker space on busy host ([#71](https://github.com/advoq/civm/issues/71)) ([d28a067](https://github.com/advoq/civm/commit/d28a067c0d4098a95d8f6a1b2ee4d77c476fc505))
* **cleanup:** render benign deferrals as deferido ([#73](https://github.com/advoq/civm/issues/73)) ([09428fc](https://github.com/advoq/civm/commit/09428fcd9a9493c7b11e5d45f4da85c1c83652ae))

## [1.11.2](https://github.com/advoq/civm/compare/v1.11.1...v1.11.2) (2026-06-01)


### Bug Fixes

* **runner:** treat host-busy watchdog skip as success ([#68](https://github.com/advoq/civm/issues/68)) ([f0b0760](https://github.com/advoq/civm/commit/f0b0760940e5c2920511f5b598a9f21e8494be9f))

## [1.11.1](https://github.com/advoq/civm/compare/v1.11.0...v1.11.1) (2026-05-31)


### Bug Fixes

* **infra:** harden host vhdx autoreclaim ([4cd6f87](https://github.com/advoq/civm/commit/4cd6f87acbcb0764016c296c17f99a539b451c93))
* **infra:** harden host vhdx autoreclaim ([aa7357d](https://github.com/advoq/civm/commit/aa7357dde763af0afa0eb2eed79aba2b11ade8c3))


### Documentation

* refresh SSDV3 to latest workspace model ([1fd25d3](https://github.com/advoq/civm/commit/1fd25d384f1d94f1c2e4aa708b8a0f7df6cea2b1))
* refresh SSDV3 to latest workspace model ([89569f9](https://github.com/advoq/civm/commit/89569f97f4bfe8950adf6c93ea11f5777c2a3bde))

## [1.11.0](https://github.com/advoq/civm/compare/v1.10.0...v1.11.0) (2026-05-31)


### Features

* **runner:** harden privileged primitives + sync Kahneman discipline [#13](https://github.com/advoq/civm/issues/13) ([#63](https://github.com/advoq/civm/issues/63)) ([f222f4d](https://github.com/advoq/civm/commit/f222f4d41f38313f23e0a09911b12b4643a7e0b9))


### Bug Fixes

* **safedelete:** escalate root-owned _work targets and gate it for real ([#61](https://github.com/advoq/civm/issues/61)) ([61e4450](https://github.com/advoq/civm/commit/61e44504f37d3b8a8113869362049412e8b73376))

## [1.10.0](https://github.com/advoq/civm/compare/v1.9.0...v1.10.0) (2026-05-30)


### Features

* **runner:** privileged-safe cleanup of root-owned runner _work ([#59](https://github.com/advoq/civm/issues/59)) ([26739ae](https://github.com/advoq/civm/commit/26739aedfb4f590b33f96658cb79fd5c3af50efd))

## [1.9.0](https://github.com/advoq/civm/compare/v1.8.2...v1.9.0) (2026-05-29)


### Features

* **runner:** multi-project isolation and host-volume reclamation ([#57](https://github.com/advoq/civm/issues/57)) ([ddd569e](https://github.com/advoq/civm/commit/ddd569e32e45500aaebdddde0c65aaceae14a5a3))

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
