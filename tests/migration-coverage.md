# Migration coverage: upstream suite → cc-router

Every assertion site in dotfiles' `scripts/test-cc-harness-agents.sh` and where
it lives now. Source: dotfiles `f429346` (blob `bc51bee4c77f`), the bash
implementation's suite at the extraction baseline.

Counts are **assertion call sites**, not executed assertions: loops and
`assert_secret_safe` (three checks) expand at run time. The suite here runs
389 assertions (`tests/run`); upstream's figure included wrapper and parity
checks that stay in dotfiles.

| Disposition | Sites |
|---|---|
| ported verbatim | 253 |
| stays in dotfiles (wrapper, statusline parity, zsh functions) | 58 |
| ported with a harness adaptation (noted per row) | 10 |
| replaced by an equivalent behavioural check | 5 |
| retired (curl-specific, justified per row) | 2 |
| **total** | **328** |

**How the harness changed.** The bash helper shelled out to curl and nc, which
PATH fakes intercepted; the Go binary does HTTPS and TCP itself. A loopback
test gateway (`tests/gateway`) now plays the server, and `run_capture`
translates the upstream `FAKE_CURL_*` / `FAKE_NC_*` knobs into test-gateway state,
so the call sites and assertion bodies stay as they were. Remote-route runs use
`tests/testdriver` (production code trusting only the test gateway CA — certificate
verification stays on); the production binary's own remote path is covered by
`tests/contract_test.go`.

**Added beyond upstream** (previously untested): credential verdicts and
newest-usable selection (`TestCredVerdict`, `TestCheckCredsNewestUsableWins`),
marker edge cases, TLS untrusted/hostname, timeouts, chunked body cap, proxy
precedence, true PID replacement, exit and signal propagation, exact argv,
bare vs. namespaced ids, symlink relocation, the `models.tsv` override, the
config file, usage and `HOME` handling.

**Implementation-specific checks retired**: curl argv (`--header @-`, flags) —
there is no curl; missing `jq`/`curl`/`nc` branches — the binary has no such
runtime dependencies.

| Upstream line | Assertion | Here |
|---|---|---|
| 313 | remote list succeeds | ported verbatim |
| 314 | remote list probes exactly once | ported verbatim |
| 315 | remote list marks a returned model available | ported verbatim |
| 316 | remote list marks a missing model unavailable | ported verbatim |
| 317 | remote list emits every table row | ported verbatim |
| 318 | a captured listing carries no header line | ported verbatim |
| 333 | the wrapper has a --$agent selector | stays in dotfiles: row ↔ wrapper/statusline parity (wrapper and statusline are not extracted yet) |
| 335 | the wrapper has a --$agent selector | stays in dotfiles: row ↔ wrapper/statusline parity (wrapper and statusline are not extracted yet) |
| 361 | an explicit header is accepted | ported verbatim |
| 362 | the header names all four columns | ported verbatim |
| 363 | the header leaves every agent row intact | ported verbatim |
| 366 | every available column starts at one shared offset | ported verbatim |
| 367 | short names are padded to the widest row | ported verbatim |
| 374 | an explicit no-header is accepted | ported verbatim |
| 375 | no-header keeps the local listing at one line per agent | ported verbatim |
| 381 | an unknown list flag is a usage error | ported verbatim |
| 382 | usage documents the header flags | ported verbatim |
| 397 | the default captured listing succeeds | ported verbatim |
| 398 | the default captured listing starts with an agent row | ported verbatim |
| 399 | remote probe uses the fixed models URL | replaced: the test gateway asserts `GET /c/<case>/v1/models` on the configured gateway |
| 400 | remote probe disables redirects | replaced: redirect case (302 reported, target never requested) in the suite + TestProbeClassifiesFailures |
| 401 | remote probe caps the response body at 1 MiB | replaced: 1 MiB body-cap case in the suite + TestProbeClassifiesFailures (incl. chunked) |
| 402 | remote probe restricts the protocol to HTTPS | replaced: plaintext URL refused with exit 3 and no request (suite) + TestSettingValidation |
| 403 | remote probe tells curl to read headers from stdin | retired: curl-specific transport (`--header @-`); the Go probe builds headers in memory and spawns no process. Security intent kept by the test gateway header assertions and TestProductionRemoteRoute (no secret in diagnostics) |
| 404 | remote probe sends bearer authentication | ported (reads the test gateway request log) |
| 405 | remote probe sends the Access client ID | ported (test-gateway log; header name canonicalized by the server) |
| 406 | remote probe sends the Access client secret | ported (test-gateway log; header name canonicalized by the server) |
| 407 | curl argv hides probe secrets | retired: there is no curl argv; no child process exists in the probe |
| 408 | remote probe never enables redirect following | replaced: redirect case (never followed) |
| 409 | remote list stderr hides secrets | ported verbatim |
| 421 | explicit profile override succeeds | ported verbatim |
| 422 | explicit profile resolves its API key indirectly | ported verbatim |
| 423 | explicit profile resolves its Access ID indirectly | ported verbatim |
| 424 | explicit profile secret is absent from stderr | ported verbatim |
| 440 | remote exec succeeds | ported verbatim |
| 441 | remote exec probes exactly once | ported verbatim |
| 442 | remote exec exports the fixed HTTPS base URL | ported; asserts the configured test-gateway URL instead of the personal host |
| 443 | remote exec exports the selected API key | ported verbatim |
| 444 | remote exec identifies the route | ported verbatim |
| 445 | remote exec preserves NO_PROXY | ported verbatim |
| 446 | remote exec preserves no_proxy | ported verbatim |
| 447 | remote header merge preserves order and canonicalizes Access headers | ported verbatim |
| 448 | remote header merge removes inherited Access ID | ported verbatim |
| 449 | remote header merge removes inherited Access secret | ported verbatim |
| 450 | remote exec clears provider selectors | ported verbatim |
| 451 | remote exec clears the gateway selector | ported verbatim |
| 452 | remote exec clears the Anthropic Google selector | ported verbatim |
| 453 | remote exec exports the primary model | ported verbatim |
| 454 | remote exec preserves target argv | ported verbatim |
| 455 | remote exec stderr hides secrets | ported verbatim |
| 478 | remote headers reject $header_case input | ported verbatim |
| 479 | remote header $header_case error hides secrets | ported verbatim |
| 488 | local list succeeds | ported verbatim |
| 489 | local list uses only the reachability probe | ported verbatim |
| 490 | local list keeps provider availability checks | ported verbatim |
| 501 | local exec succeeds | ported verbatim |
| 502 | local exec uses only the reachability probe | ported verbatim |
| 503 | local exec exports the loopback gateway | ported; asserts the test gateway port instead of the fixed 8317 |
| 504 | local exec exports the local token | ported verbatim |
| 505 | local exec identifies the route | ported verbatim |
| 506 | loopback bypass is added only locally | ported verbatim |
| 507 | local lowercase bypass matches the merged list | ported verbatim |
| 508 | local exec preserves non-Access custom headers | ported verbatim |
| 509 | local exec removes every inherited Access ID | ported verbatim |
| 510 | local exec removes every inherited Access secret | ported verbatim |
| 511 | local exec clears the gateway selector | ported verbatim |
| 512 | local exec clears the Anthropic Google selector | ported verbatim |
| 519 | local exec rejects an unreachable loopback gateway | ported verbatim |
| 520 | local reachability error names the local gateway | ported; asserts the test gateway port instead of the fixed 8317 |
| 521 | local reachability failure never invokes curl | ported verbatim |
| 537 | local headers reject $header_case input | ported verbatim |
| 548 | local list still exits 0 without a fallback marker | ported verbatim |
| 549 | a missing marker still lists every row | ported verbatim |
| 550 | a missing marker marks every row unavailable | ported verbatim |
| 551 | a blocked local listing still labels the last-known model | ported verbatim |
| 552 | and stays at four columns | ported verbatim |
| 553 | the missing marker note names the fix | ported; generic default hint (personal `--mars-stopped` flag is configuration now, see TestConfigFile) |
| 561 | local exec refuses without a fallback marker | ported verbatim |
| 562 | the exec refusal names the fix | ported; generic default hint |
| 570 | local exec refuses an expired fallback marker | ported verbatim |
| 571 | the expired marker is named as expired | ported verbatim |
| 572 | the expired marker names the fix | ported; generic default hint |
| 580 | local list reports an expired marker per row | ported verbatim |
| 589 | local exec refuses a corrupt fallback marker | ported verbatim |
| 590 | a corrupt marker is reported as unusable | ported verbatim |
| 599 | the marker gate precedes the token and gateway probes | ported verbatim |
| 600 | the marker reason wins over the token reason | ported verbatim |
| 601 | the token probe never ran | ported verbatim |
| 602 | the gateway probe never ran | ported verbatim |
| 611 | a prepared fallback still requires the gateway token | ported verbatim |
| 612 | the token probe runs once the marker is valid | ported verbatim |
| 633 | remote probe failure exits 1 ($expected) | ported verbatim |
| 634 | remote probe classifies $expected | ported verbatim |
| 635 | failed remote exec still probes exactly once ($expected) | ported; request count observed by the test gateway — 0 for the two transport failures (the request never arrives; for TLS the secrets never reach the peer) |
| 636 | remote $expected error hides secrets | ported verbatim |
| 648 | remote exec rejects a missing requested model | ported verbatim |
| 649 | missing-model error names only the model | ported verbatim |
| 650 | missing-model stderr hides secrets | ported verbatim |
| 661 | remote list completes when a tier model is missing | ported verbatim |
| 662 | remote list validates exported tier models | ported verbatim |
| 663 | missing Codex tier does not affect unrelated rows | ported verbatim |
| 664 | missing-tier stderr hides secrets | ported verbatim |
| 680 | remote list succeeds with a full catalog | ported verbatim |
| 681 | a complete catalog makes astra available | ported verbatim |
| 694 | remote exec runs the astra row | ported verbatim |
| 695 | astra exec exports its own primary | ported verbatim |
| 696 | astra opus rung is the primary itself | ported verbatim |
| 697 | astra sonnet rung mirrors the primary, so it holds the announced window | ported verbatim |
| 698 | astra haiku rung is the verified 5.6 model, not the unverified spark id | ported verbatim |
| 699 | astra exports the measured context ceiling | ported verbatim |
| 700 | astra exec stderr hides secrets | ported verbatim |
| 713 | remote list completes when astra is absent from the catalog | ported verbatim |
| 714 | an absent astra primary marks only its own row unavailable | ported verbatim |
| 715 | an absent astra leaves unrelated rows alone | ported verbatim |
| 725 | remote list completes when the astra haiku rung is absent | ported verbatim |
| 728 | astra validates its own exported tier | ported verbatim |
| 729 | the shared id takes the luna row down with it | ported verbatim |
| 730 | the astra tier gap does not touch the sol row | ported verbatim |
| 739 | invalid profile syntax is a capability error | ported verbatim |
| 740 | invalid profile syntax has a safe diagnostic | ported verbatim |
| 750 | control characters in remote secrets are rejected | ported verbatim |
| 751 | control-character error is generic | ported verbatim |
| 752 | secret-validation stderr hides secrets | ported verbatim |
| 762 | empty remote credentials are rejected | ported verbatim |
| 763 | empty-credential error is generic | ported verbatim |
| 764 | empty-credential stderr hides other secrets | ported verbatim |
| 828 | zsh wrapper routes foreign models remotely by default | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 829 | zsh remote prefix omits --local | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 830 | zsh remote route preserves prompt argv | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 833 | zsh wrapper accepts --local with a foreign model | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 834 | zsh local prefix delegates route selection to helper | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 837 | zsh wrapper treats --local after bare -- as passthrough | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 838 | passthrough --local does not select local routing | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 839 | passthrough --local reaches Claude argv | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 842 | zsh wrapper rejects --local without a foreign model | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 843 | zsh wrapper explains bare --local rejection | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 846 | zsh wrapper keeps native-model conflict detection | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 847 | zsh wrapper reports native-model conflict | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 850 | zsh wrapper keeps foreign-model conflict detection | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 851 | zsh wrapper reports foreign-model conflict | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 862 | zsh wrapper accepts --astra | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 863 | --astra hands the row name straight to the helper | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 864 | the selector is consumed, never leaked into Claude argv | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 865 | --astra preserves the prompt argv | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 871 | a plain launch still succeeds | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 872 | a plain launch never reaches the routing helper | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 880 | zsh wrapper accepts --$native_case | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 882 | "$(<"$WRAPPER_CLAUDE_LOG")" "--model | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 884 | the shorthand itself never reaches Claude argv | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 885 | --$native_case is native, so it never routes | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 889 | a native shorthand conflicts with a foreign flag | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 892 | the conflict names the shorthand, not --model | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 893 | a shorthand conflict never blames a bare --model | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 896 | a bare --model still conflicts with a foreign flag | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 897 | a bare --model is still named as itself | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 900 | a native shorthand conflicts with an explicit --model | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 901 | the shorthand/--model conflict is reported | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 904 | the conflict is caught in either order | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 907 | two different native shorthands conflict | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 908 | the native/native conflict is reported | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 911 | repeating the same native shorthand is not a conflict | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 912 | a repeated shorthand is only expanded once | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 915 | a literal --opus after bare -- is passthrough | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 916 | passthrough --opus reaches Claude argv | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 928 | zsh wrapper script path succeeds | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 930 | zsh wrapper pins the production system script path | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 932 | zsh wrapper pins the production system script path | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 934 | isolated resume test uses its deterministic script fake | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 936 | zsh wrapper never executes PATH script | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 938 | zsh wrapper never executes PATH script | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 940 | resume history preserves yolo, local route, and model | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 964 | $zsh_function_rel exists | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 966 | .zshrc sources $zsh_function_rel | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 968 | .zshrc sources $zsh_function_rel | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 974 | .zshrc no longer defines $zsh_function_name() itself | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 976 | .zshrc no longer defines $zsh_function_name() itself | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 987 | the function files load in a bare zsh | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 988 | listening.zsh defines listening | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 989 | claude.zsh defines claude | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 990 | sourcing the function files stays silent | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 996 | listening rejects more than one argument | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 997 | listening explains its usage | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 1042 | discovery over the live catalog succeeds | ported verbatim |
| 1043 | the live catalog selects grok-4.6, its newest candidate, with no note | ported verbatim |
| 1044 | no 4.20 variant is ever selected | ported verbatim |
| 1045 | reasoning and non-reasoning variants stay out of the selection | ported verbatim |
| 1046 | multi-agent variants stay out of the selection | ported verbatim |
| 1047 | a superseded major version is never selected | ported verbatim |
| 1057 | exec over the live catalog succeeds | ported verbatim |
| 1058 | the live ladder puts the newest model on the primary | ported verbatim |
| 1059 | the live ladder puts the newest model on the opus tier | ported verbatim |
| 1060 | the live ladder puts the predecessor on the sonnet tier | ported verbatim |
| 1061 | the haiku tier stays on composer | ported verbatim |
| 1071 | exec with a single canonical model succeeds | ported verbatim |
| 1072 | a lone canonical model is the primary | ported verbatim |
| 1073 | the sonnet tier mirrors the primary when nothing older exists | ported verbatim |
| 1083 | an override sets the primary | ported verbatim |
| 1084 | an override sets the opus tier | ported verbatim |
| 1085 | an override collapses the sonnet tier onto the same model | ported verbatim |
| 1086 | an override still leaves the haiku tier alone | ported verbatim |
| 1096 | a future 4.x is selected unedited, with candidates and the assumed window named | ported verbatim |
| 1098 | a selection note keeps the row at four columns | ported verbatim |
| 1105 | exec with a future release succeeds | ported verbatim |
| 1106 | exec exports exactly the model list reported for the same catalog | ported verbatim |
| 1107 | the subagent model follows the selection | ported verbatim |
| 1108 | the opus tier follows the selection | ported verbatim |
| 1109 | the sonnet tier takes the rung below the new primary | ported verbatim |
| 1110 | a tier pointed elsewhere never follows the selection | ported verbatim |
| 1111 | a future release is given its predecessor's window | ported verbatim |
| 1112 | the selection note never leaks into the target argv | ported verbatim |
| 1113 | exec names the automatic selection on stderr | ported verbatim |
| 1114 | exec names the assumption on stderr, every run | ported verbatim |
| 1115 | the selection note carries no secret | ported verbatim |
| 1124 | 5.0 outranks every 4.x | ported verbatim |
| 1125 | 4.20 outranks 4.9 by component, not by decimal value | ported verbatim |
| 1132 | 4.20 is the newest of 4.6, 4.9 and 4.20 | ported verbatim |
| 1133 | candidates are listed newest first | ported verbatim |
| 1146 | bare majors, other majors, patch ids, variants and malformed ids are never selected | ported verbatim |
| 1147 | and none of them is reported as a candidate | ported verbatim |
| 1157 | an overlong version component does not abort the listing | ported verbatim |
| 1158 | an overlong version component is not a candidate | ported verbatim |
| 1166 | a zero-padded minor does not abort the listing | ported verbatim |
| 1167 | a zero-padded minor compares as a decimal number | ported verbatim |
| 1186 | two unknown releases: the newest is selected | ported verbatim |
| 1187 | an assumption never chains from another assumption | ported verbatim |
| 1188 | a rung with the same assumed window holds the ceiling | ported verbatim |
| 1197 | a smaller catalog limit is respected, not overridden by the assumption | ported verbatim |
| 1198 | an explicit limit is not reported as an assumption | ported verbatim |
| 1199 | a larger rung still holds the smaller ceiling | ported verbatim |
| 1208 | the first release of a new major is selected | ported verbatim |
| 1209 | and gets at least the window of the newest verified 4.x | ported verbatim |
| 1210 | named as an assumption across the major boundary too | ported verbatim |
| 1211 | the verified 4.x rung holds the assumed ceiling | ported verbatim |
| 1221 | a release below every verified one has no predecessor to assume from | ported verbatim |
| 1229 | a variant never inherits a canonical release's window | ported verbatim |
| 1237 | grok-4.7 is selected from the catalog | ported verbatim |
| 1238 | with a documented window, not an assumed one | ported verbatim |
| 1249 | a release with catalog metadata is selected | ported verbatim |
| 1250 | catalog metadata supplies the ceiling | ported verbatim |
| 1251 | a rung that holds the announced ceiling is kept | ported verbatim |
| 1252 | a known window is not called unknown | ported verbatim |
| 1253 | an advertised ceiling is named as advertised | ported verbatim |
| 1264 | context_length $float_ctx does not fail the probe | ported verbatim |
| 1265 | an integral $float_ctx is read as 300000, never inflated to the cap | ported verbatim |
| 1266 | context_length $float_ctx never reaches a shell comparison raw | ported verbatim |
| 1275 | an advertised window is clamped to the largest measured one | ported verbatim |
| 1286 | a measured window outranks catalog metadata | ported verbatim |
| 1296 | the primary's window is announced | ported verbatim |
| 1297 | a rung with a smaller window is skipped rather than overflowed | ported verbatim |
| 1310 | context_length $bad_ctx does not fail the probe | ported verbatim |
| 1311 | context_length $bad_ctx leaves the window unknown | ported verbatim |
| 1323 | a separator-bearing id does not displace the real candidate | ported verbatim |
| 1324 | a separator-bearing id cannot forge another id's window | ported verbatim |
| 1332 | a variant's advertised window is never believed | ported verbatim |
| 1340 | an agent without discovery never reads catalog metadata | ported verbatim |
| 1351 | a valid catalog below the last-known model selects its newest candidate | ported verbatim |
| 1359 | exec follows a catalog below the last-known model | ported verbatim |
| 1360 | exec exports what the catalog offers, not the stale last-known model | ported verbatim |
| 1361 | the rung below follows downwards too | ported verbatim |
| 1362 | a measured older release keeps its measured window | ported verbatim |
| 1372 | no eligible candidate: unavailable, nothing substituted, fallback labelled | ported verbatim |
| 1380 | exec refuses when the catalog offers no candidate | ported verbatim |
| 1381 | nothing is exec'd on a last-known model the route does not offer | ported verbatim |
| 1382 | the refusal names the fallback for what it is | ported verbatim |
| 1391 | an unavailable catalog labels the last-known model | ported verbatim |
| 1392 | a row without discovery carries no fallback label | ported verbatim |
| 1393 | the label is not sprayed over rows that never discover | ported verbatim |
| 1400 | an unparsable catalog is stale, not empty | ported verbatim |
| 1407 | exec never runs on a stale catalog | ported verbatim |
| 1408 | no model is exported without a catalog on the remote route | ported verbatim |
| 1417 | a missing haiku tier keeps the row unavailable after discovery | ported verbatim |
| 1427 | a pin the catalog lacks is reported missing, never swapped for the latest | ported verbatim |
| 1437 | a session recorded on grok-4.6 resumes on grok-4.6 while 4.7 is offered | ported verbatim |
| 1438 | and keeps the ladder it was started with | ported verbatim |
| 1439 | and its measured window | ported verbatim |
| 1447 | a session recorded on an auto-selected release resumes on that exact release | ported verbatim |
| 1451 | a restored non-table release collapses the sonnet rung, as every override does | ported verbatim |
| 1452 | and leaves the haiku rung alone | ported verbatim |
| 1453 | with the same assumed ceiling a fresh start gets | ported verbatim |
| 1454 | and the assumption is named on a resume too | ported verbatim |
| 1458 | resolve-model routes a future release to grok | ported verbatim |
| 1460 | resolve-model normalises a future build id | ported verbatim |
| 1476 | the local route lists the last-known model and says so | ported verbatim |
| 1477 | the local route fetches no catalog | ported verbatim |
| 1478 | the native grok CLI is never consulted locally | ported verbatim |
| 1492 | a native CLI that lists a newer model does not move the remote route | ported verbatim |
| 1493 | the native grok CLI is never consulted remotely | ported verbatim |
| 1494 | discovery still costs exactly one request | ported verbatim |
| 1503 | an override wins over the catalog and says so | ported verbatim |
| 1513 | an override to a variant model still execs | ported verbatim |
| 1514 | an override reaches the environment verbatim | ported verbatim |
| 1515 | an unverified override gets the conservative ceiling | ported verbatim |
| 1516 | the unknown ceiling is stated on stderr | ported verbatim |
| 1525 | a malformed override is reported | ported verbatim |
| 1526 | a malformed override leaves the pin in place | ported verbatim |
| 1535 | local exec with an override succeeds | ported verbatim |
| 1536 | the local route honours an explicit override | ported verbatim |
| 1537 | a verified override keeps its real ceiling on the local route | ported verbatim |
| 1544 | the local route lists the deterministic pinned model | ported verbatim |
| 1555 | a tier override execs | ported verbatim |
| 1556 | the tier override reaches the environment | ported verbatim |
| 1557 | a verified tier keeps its own real ceiling | ported verbatim |
| 1558 | a model the table declares is never called unverified | ported verbatim |
| 1568 | pinning the primary stays quiet | ported verbatim |
| 1581 | pinning the astra primary execs | ported verbatim |
| 1582 | the resumed primary is the pinned one | ported verbatim |
| 1583 | pinning the primary leaves the sonnet rung on the table value | ported verbatim |
| 1584 | pinning the primary leaves the haiku rung alone | ported verbatim |
| 1585 | pinning the primary keeps the row ceiling | ported verbatim |
| 1595 | an override to another model still wins | ported verbatim |
| 1596 | the tracking rung collapses onto a real override | ported verbatim |
| 1597 | a rung the override does not name is never rewritten | ported verbatim |
| 1609 | a smaller tier never inherits the row ceiling | ported verbatim |
| 1618 | an undocumented tier gets the conservative ceiling | ported verbatim |
| 1619 | and the unknown ceiling is stated | ported verbatim |
| 1626 | resolve-model resolves a primary | ported verbatim |
| 1627 | a shared-tier agent is named by its primary | ported verbatim |
| 1631 | resolve-model resolves a tier model | ported verbatim |
| 1639 | resolve-model accepts the astra primary | ported verbatim |
| 1640 | resolve-model maps the astra primary to its row | ported verbatim |
| 1642 | the borrowed haiku rung still resolves to luna | ported verbatim |
| 1649 | a tier shared by three rows still resolves | ported verbatim |
| 1650 | the shared tier keeps its own id in the answer | ported verbatim |
| 1654 | an xAI build id normalises to the proxy alias | ported verbatim |
| 1658 | an unknown id routes when one agent claims the prefix | ported verbatim |
| 1662 | an unknown gpt id is refused rather than guessed | ported verbatim |
| 1663 | the refusal says why | ported verbatim |
| 1667 | a native model has no agent profile | ported verbatim |
