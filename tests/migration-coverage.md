# Migration coverage: upstream suite → cc-router

Every assertion site in dotfiles' `scripts/test-cc-harness-agents.sh` and where
it lives now. Source: dotfiles `de7a736` (blob `64971056da90`), the bash
implementation's suite at the equivalence baseline (first mapped at `f429346`;
the delta is summarized below).

Counts are **assertion call sites**, not executed assertions: loops and
`assert_secret_safe` (three checks) expand at run time. The suite here runs
420 assertions (`tests/run`); upstream's figure included wrapper and parity
checks that stay in dotfiles.

| Disposition | Sites |
|---|---|
| ported verbatim | 258 |
| stays in dotfiles (wrapper, statusline parity, zsh functions) | 58 |
| ported with a harness adaptation (noted per row) | 14 |
| replaced by an equivalent behavioural check | 5 |
| retired (curl-specific, justified per row) | 2 |
| **total** | **337** |

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

**Delta `f429346` → `de7a736`** (dotfiles #44, Codex tier routing without
the retired Spark model): a tenth table column `fable`
(`ANTHROPIC_DEFAULT_FABLE_MODEL`), one Codex ladder in all four codex rows at a
shared 372000 ceiling, and an override ceiling capped at the row's. Ported:
the exported-ladder loop, the catalog-only gpt-6 ids that move no rung, the
missing-family and shared-haiku-rung cases, the unmeasured gpt-6 windows, the
override cap, and resolve-model refusing retired or not-yet-carried ids. The
shared-tier resolve case upstream drives through a rewritten copy of the script;
here it is a user `models.tsv` (adapted). Every upstream site is mapped.

| Upstream line | Assertion | Here |
|---|---|---|
| 316 | remote list succeeds | ported verbatim |
| 317 | remote list probes exactly once | ported verbatim |
| 318 | remote list marks a returned model available | ported verbatim |
| 319 | remote list marks a missing model unavailable | ported verbatim |
| 320 | remote list emits every table row | ported verbatim |
| 321 | a captured listing carries no header line | ported verbatim |
| 336 | the wrapper has a --$agent selector | stays in dotfiles: row ↔ wrapper/statusline parity (wrapper and statusline are not extracted yet) |
| 338 | the wrapper has a --$agent selector | stays in dotfiles: row ↔ wrapper/statusline parity (wrapper and statusline are not extracted yet) |
| 364 | an explicit header is accepted | ported verbatim |
| 365 | the header names all four columns | ported verbatim |
| 366 | the header leaves every agent row intact | ported verbatim |
| 369 | every available column starts at one shared offset | ported verbatim |
| 370 | short names are padded to the widest row | ported verbatim |
| 377 | an explicit no-header is accepted | ported verbatim |
| 378 | no-header keeps the local listing at one line per agent | ported verbatim |
| 384 | an unknown list flag is a usage error | ported verbatim |
| 385 | usage documents the header flags | ported verbatim |
| 400 | the default captured listing succeeds | ported verbatim |
| 401 | the default captured listing starts with an agent row | ported verbatim |
| 402 | remote probe uses the fixed models URL | replaced: the test gateway asserts `GET /c/<case>/v1/models` on the configured gateway |
| 403 | remote probe disables redirects | replaced: redirect case (302 reported, target never requested) in the suite + TestProbeClassifiesFailures |
| 404 | remote probe caps the response body at 1 MiB | replaced: 1 MiB body-cap case in the suite + TestProbeClassifiesFailures (incl. chunked) |
| 405 | remote probe restricts the protocol to HTTPS | replaced: plaintext URL refused with exit 3 and no request (suite) + TestSettingValidation |
| 406 | remote probe tells curl to read headers from stdin | retired: curl-specific transport (`--header @-`); the Go probe builds headers in memory and spawns no process. Security intent kept by the test gateway header assertions and TestProductionRemoteRoute (no secret in diagnostics) |
| 407 | remote probe sends bearer authentication | ported (reads the test gateway request log) |
| 408 | remote probe sends the Access client ID | ported (test-gateway log; header name canonicalized by the server) |
| 409 | remote probe sends the Access client secret | ported (test-gateway log; header name canonicalized by the server) |
| 410 | curl argv hides probe secrets | retired: there is no curl argv; no child process exists in the probe |
| 411 | remote probe never enables redirect following | replaced: redirect case (never followed) |
| 412 | remote list stderr hides secrets | ported verbatim |
| 424 | explicit profile override succeeds | ported verbatim |
| 425 | explicit profile resolves its API key indirectly | ported verbatim |
| 426 | explicit profile resolves its Access ID indirectly | ported verbatim |
| 427 | explicit profile secret is absent from stderr | ported verbatim |
| 443 | remote exec succeeds | ported verbatim |
| 444 | remote exec probes exactly once | ported verbatim |
| 445 | remote exec exports the fixed HTTPS base URL | ported; asserts the configured test-gateway URL instead of the personal host |
| 446 | remote exec exports the selected API key | ported verbatim |
| 447 | remote exec identifies the route | ported verbatim |
| 448 | remote exec preserves NO_PROXY | ported verbatim |
| 449 | remote exec preserves no_proxy | ported verbatim |
| 450 | remote header merge preserves order and canonicalizes Access headers | ported verbatim |
| 451 | remote header merge removes inherited Access ID | ported verbatim |
| 452 | remote header merge removes inherited Access secret | ported verbatim |
| 453 | remote exec clears provider selectors | ported verbatim |
| 454 | remote exec clears the gateway selector | ported verbatim |
| 455 | remote exec clears the Anthropic Google selector | ported verbatim |
| 456 | remote exec exports the primary model | ported verbatim |
| 457 | remote exec preserves target argv | ported verbatim |
| 458 | remote exec stderr hides secrets | ported verbatim |
| 481 | remote headers reject $header_case input | ported verbatim |
| 482 | remote header $header_case error hides secrets | ported verbatim |
| 491 | local list succeeds | ported verbatim |
| 492 | local list uses only the reachability probe | ported verbatim |
| 493 | local list keeps provider availability checks | ported verbatim |
| 504 | local exec succeeds | ported verbatim |
| 505 | local exec uses only the reachability probe | ported verbatim |
| 506 | local exec exports the loopback gateway | ported; asserts the test gateway port instead of the fixed 8317 |
| 507 | local exec exports the local token | ported verbatim |
| 508 | local exec identifies the route | ported verbatim |
| 509 | loopback bypass is added only locally | ported verbatim |
| 510 | local lowercase bypass matches the merged list | ported verbatim |
| 511 | local exec preserves non-Access custom headers | ported verbatim |
| 512 | local exec removes every inherited Access ID | ported verbatim |
| 513 | local exec removes every inherited Access secret | ported verbatim |
| 514 | local exec clears the gateway selector | ported verbatim |
| 515 | local exec clears the Anthropic Google selector | ported verbatim |
| 522 | local exec rejects an unreachable loopback gateway | ported verbatim |
| 523 | local reachability error names the local gateway | ported; asserts the test gateway port instead of the fixed 8317 |
| 524 | local reachability failure never invokes curl | ported verbatim |
| 540 | local headers reject $header_case input | ported verbatim |
| 551 | local list still exits 0 without a fallback marker | ported verbatim |
| 552 | a missing marker still lists every row | ported verbatim |
| 553 | a missing marker marks every row unavailable | ported verbatim |
| 554 | a blocked local listing still labels the last-known model | ported verbatim |
| 555 | and stays at four columns | ported verbatim |
| 556 | the missing marker note names the fix | ported; generic default hint (personal `--mars-stopped` flag is configuration now, see TestConfigFile) |
| 564 | local exec refuses without a fallback marker | ported verbatim |
| 565 | the exec refusal names the fix | ported; generic default hint |
| 573 | local exec refuses an expired fallback marker | ported verbatim |
| 574 | the expired marker is named as expired | ported verbatim |
| 575 | the expired marker names the fix | ported; generic default hint |
| 583 | local list reports an expired marker per row | ported verbatim |
| 592 | local exec refuses a corrupt fallback marker | ported verbatim |
| 593 | a corrupt marker is reported as unusable | ported verbatim |
| 602 | the marker gate precedes the token and gateway probes | ported verbatim |
| 603 | the marker reason wins over the token reason | ported verbatim |
| 604 | the token probe never ran | ported verbatim |
| 605 | the gateway probe never ran | ported verbatim |
| 614 | a prepared fallback still requires the gateway token | ported verbatim |
| 615 | the token probe runs once the marker is valid | ported verbatim |
| 636 | remote probe failure exits 1 ($expected) | ported verbatim |
| 637 | remote probe classifies $expected | ported verbatim |
| 638 | failed remote exec still probes exactly once ($expected) | ported; request count observed by the test gateway — 0 for the two transport failures (the request never arrives; for TLS the secrets never reach the peer) |
| 639 | remote $expected error hides secrets | ported verbatim |
| 651 | remote exec rejects a missing requested model | ported verbatim |
| 652 | missing-model error names only the model | ported verbatim |
| 653 | missing-model stderr hides secrets | ported verbatim |
| 664 | remote list completes when a tier model is missing | ported verbatim |
| 668 | remote list validates the shared haiku rung (${agent_model%%:*}) | ported verbatim |
| 670 | missing Codex tier does not affect unrelated rows | ported verbatim |
| 671 | missing-tier stderr hides secrets | ported verbatim |
| 688 | remote list succeeds with a full catalog | ported verbatim |
| 689 | a complete catalog makes astra available | ported verbatim |
| 709 | $agent execs against a catalog without spark | ported verbatim |
| 710 | $agent exports its own primary | ported verbatim |
| 711 | $agent exports the shared Astra/Sol/Terra/Luna ladder | ported verbatim |
| 712 | $agent exports the smallest rung's window | ported verbatim |
| 713 | $agent execs without a note (nothing selected, nothing assumed) | ported verbatim |
| 730 | a catalog with gpt-6 sol/luna and variants still execs sol | ported verbatim |
| 731 | catalog-only gpt-6 ids move no codex rung | ported verbatim |
| 732 | catalog-only gpt-6 ids leave the ceiling alone | ported verbatim |
| 746 | a missing Terra family makes even astra unavailable | ported verbatim |
| 747 | the missing family is named | ported verbatim |
| 761 | remote list completes when astra is absent from the catalog | ported verbatim |
| 762 | an absent astra primary marks its row unavailable | ported verbatim |
| 763 | an absent astra takes the sol row's fable rung with it | ported verbatim |
| 764 | an absent astra leaves unrelated rows alone | ported verbatim |
| 773 | invalid profile syntax is a capability error | ported verbatim |
| 774 | invalid profile syntax has a safe diagnostic | ported verbatim |
| 784 | control characters in remote secrets are rejected | ported verbatim |
| 785 | control-character error is generic | ported verbatim |
| 786 | secret-validation stderr hides secrets | ported verbatim |
| 796 | empty remote credentials are rejected | ported verbatim |
| 797 | empty-credential error is generic | ported verbatim |
| 798 | empty-credential stderr hides other secrets | ported verbatim |
| 862 | zsh wrapper routes foreign models remotely by default | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 863 | zsh remote prefix omits --local | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 864 | zsh remote route preserves prompt argv | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 867 | zsh wrapper accepts --local with a foreign model | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 868 | zsh local prefix delegates route selection to helper | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 871 | zsh wrapper treats --local after bare -- as passthrough | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 872 | passthrough --local does not select local routing | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 873 | passthrough --local reaches Claude argv | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 876 | zsh wrapper rejects --local without a foreign model | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 877 | zsh wrapper explains bare --local rejection | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 880 | zsh wrapper keeps native-model conflict detection | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 881 | zsh wrapper reports native-model conflict | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 884 | zsh wrapper keeps foreign-model conflict detection | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 885 | zsh wrapper reports foreign-model conflict | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 896 | zsh wrapper accepts --astra | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 897 | --astra hands the row name straight to the helper | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 898 | the selector is consumed, never leaked into Claude argv | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 899 | --astra preserves the prompt argv | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 905 | a plain launch still succeeds | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 906 | a plain launch never reaches the routing helper | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 914 | zsh wrapper accepts --$native_case | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 916 | --$native_case expands to a native --model | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 918 | the shorthand itself never reaches Claude argv | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 919 | --$native_case is native, so it never routes | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 923 | a native shorthand conflicts with a foreign flag | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 926 | the conflict names the shorthand, not --model | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 927 | a shorthand conflict never blames a bare --model | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 930 | a bare --model still conflicts with a foreign flag | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 931 | a bare --model is still named as itself | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 934 | a native shorthand conflicts with an explicit --model | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 935 | the shorthand/--model conflict is reported | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 938 | the conflict is caught in either order | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 941 | two different native shorthands conflict | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 942 | the native/native conflict is reported | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 945 | repeating the same native shorthand is not a conflict | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 946 | a repeated shorthand is only expanded once | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 949 | a literal --opus after bare -- is passthrough | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 950 | passthrough --opus reaches Claude argv | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 962 | zsh wrapper script path succeeds | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 964 | zsh wrapper pins the production system script path | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 966 | zsh wrapper pins the production system script path | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 968 | isolated resume test uses its deterministic script fake | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 970 | zsh wrapper never executes PATH script | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 972 | zsh wrapper never executes PATH script | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 974 | resume history preserves yolo, local route, and model | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 998 | $zsh_function_rel exists | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 1000 | .zshrc sources $zsh_function_rel | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 1002 | .zshrc sources $zsh_function_rel | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 1008 | .zshrc no longer defines $zsh_function_name() itself | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 1010 | .zshrc no longer defines $zsh_function_name() itself | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 1021 | the function files load in a bare zsh | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 1022 | listening.zsh defines listening | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 1023 | claude.zsh defines claude | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 1024 | sourcing the function files stays silent | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 1030 | listening rejects more than one argument | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 1031 | listening explains its usage | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 1076 | discovery over the live catalog succeeds | ported verbatim |
| 1077 | the live catalog selects grok-4.6, its newest candidate, with no note | ported verbatim |
| 1078 | no 4.20 variant is ever selected | ported verbatim |
| 1079 | reasoning and non-reasoning variants stay out of the selection | ported verbatim |
| 1080 | multi-agent variants stay out of the selection | ported verbatim |
| 1081 | a superseded major version is never selected | ported verbatim |
| 1091 | exec over the live catalog succeeds | ported verbatim |
| 1092 | the live ladder puts the newest model on the primary | ported verbatim |
| 1093 | the live ladder puts the newest model on the opus tier | ported verbatim |
| 1094 | the live ladder puts the predecessor on the sonnet tier | ported verbatim |
| 1095 | the haiku tier stays on composer | ported verbatim |
| 1105 | exec with a single canonical model succeeds | ported verbatim |
| 1106 | a lone canonical model is the primary | ported verbatim |
| 1107 | the sonnet tier mirrors the primary when nothing older exists | ported verbatim |
| 1117 | an override sets the primary | ported verbatim |
| 1118 | an override sets the opus tier | ported verbatim |
| 1119 | an override collapses the sonnet tier onto the same model | ported verbatim |
| 1120 | an override still leaves the haiku tier alone | ported verbatim |
| 1130 | a future 4.x is selected unedited, with candidates and the assumed window named | ported verbatim |
| 1132 | a selection note keeps the row at four columns | ported verbatim |
| 1139 | exec with a future release succeeds | ported verbatim |
| 1140 | exec exports exactly the model list reported for the same catalog | ported verbatim |
| 1141 | the subagent model follows the selection | ported verbatim |
| 1142 | the opus tier follows the selection | ported verbatim |
| 1143 | the sonnet tier takes the rung below the new primary | ported verbatim |
| 1144 | a tier pointed elsewhere never follows the selection | ported verbatim |
| 1145 | a future release is given its predecessor's window | ported verbatim |
| 1146 | the selection note never leaks into the target argv | ported verbatim |
| 1147 | exec names the automatic selection on stderr | ported verbatim |
| 1148 | exec names the assumption on stderr, every run | ported verbatim |
| 1149 | the selection note carries no secret | ported verbatim |
| 1158 | 5.0 outranks every 4.x | ported verbatim |
| 1159 | 4.20 outranks 4.9 by component, not by decimal value | ported verbatim |
| 1166 | 4.20 is the newest of 4.6, 4.9 and 4.20 | ported verbatim |
| 1167 | candidates are listed newest first | ported verbatim |
| 1180 | bare majors, other majors, patch ids, variants and malformed ids are never selected | ported verbatim |
| 1181 | and none of them is reported as a candidate | ported verbatim |
| 1191 | an overlong version component does not abort the listing | ported verbatim |
| 1192 | an overlong version component is not a candidate | ported verbatim |
| 1200 | a zero-padded minor does not abort the listing | ported verbatim |
| 1201 | a zero-padded minor compares as a decimal number | ported verbatim |
| 1220 | two unknown releases: the newest is selected | ported verbatim |
| 1221 | an assumption never chains from another assumption | ported verbatim |
| 1222 | a rung with the same assumed window holds the ceiling | ported verbatim |
| 1231 | a smaller catalog limit is respected, not overridden by the assumption | ported verbatim |
| 1232 | an explicit limit is not reported as an assumption | ported verbatim |
| 1233 | a larger rung still holds the smaller ceiling | ported verbatim |
| 1242 | the first release of a new major is selected | ported verbatim |
| 1243 | and gets at least the window of the newest verified 4.x | ported verbatim |
| 1244 | named as an assumption across the major boundary too | ported verbatim |
| 1245 | the verified 4.x rung holds the assumed ceiling | ported verbatim |
| 1255 | a release below every verified one has no predecessor to assume from | ported verbatim |
| 1263 | a variant never inherits a canonical release's window | ported verbatim |
| 1271 | grok-4.7 is selected from the catalog | ported verbatim |
| 1272 | with a documented window, not an assumed one | ported verbatim |
| 1283 | a release with catalog metadata is selected | ported verbatim |
| 1284 | catalog metadata supplies the ceiling | ported verbatim |
| 1285 | a rung that holds the announced ceiling is kept | ported verbatim |
| 1286 | a known window is not called unknown | ported verbatim |
| 1287 | an advertised ceiling is named as advertised | ported verbatim |
| 1298 | context_length $float_ctx does not fail the probe | ported verbatim |
| 1299 | an integral $float_ctx is read as 300000, never inflated to the cap | ported verbatim |
| 1300 | context_length $float_ctx never reaches a shell comparison raw | ported verbatim |
| 1309 | an advertised window is clamped to the largest measured one | ported verbatim |
| 1320 | a measured window outranks catalog metadata | ported verbatim |
| 1330 | the primary's window is announced | ported verbatim |
| 1331 | a rung with a smaller window is skipped rather than overflowed | ported verbatim |
| 1344 | context_length $bad_ctx does not fail the probe | ported verbatim |
| 1345 | context_length $bad_ctx leaves the window unknown | ported verbatim |
| 1357 | a separator-bearing id does not displace the real candidate | ported verbatim |
| 1358 | a separator-bearing id cannot forge another id's window | ported verbatim |
| 1366 | a variant's advertised window is never believed | ported verbatim |
| 1374 | an agent without discovery never reads catalog metadata | ported verbatim |
| 1385 | a valid catalog below the last-known model selects its newest candidate | ported verbatim |
| 1393 | exec follows a catalog below the last-known model | ported verbatim |
| 1394 | exec exports what the catalog offers, not the stale last-known model | ported verbatim |
| 1395 | the rung below follows downwards too | ported verbatim |
| 1396 | a measured older release keeps its measured window | ported verbatim |
| 1406 | no eligible candidate: unavailable, nothing substituted, fallback labelled | ported verbatim |
| 1414 | exec refuses when the catalog offers no candidate | ported verbatim |
| 1415 | nothing is exec'd on a last-known model the route does not offer | ported verbatim |
| 1416 | the refusal names the fallback for what it is | ported verbatim |
| 1425 | an unavailable catalog labels the last-known model | ported verbatim |
| 1426 | a row without discovery carries no fallback label | ported verbatim |
| 1427 | the label is not sprayed over rows that never discover | ported verbatim |
| 1434 | an unparsable catalog is stale, not empty | ported verbatim |
| 1441 | exec never runs on a stale catalog | ported verbatim |
| 1442 | no model is exported without a catalog on the remote route | ported verbatim |
| 1451 | a missing haiku tier keeps the row unavailable after discovery | ported verbatim |
| 1461 | a pin the catalog lacks is reported missing, never swapped for the latest | ported verbatim |
| 1471 | a session recorded on grok-4.6 resumes on grok-4.6 while 4.7 is offered | ported verbatim |
| 1472 | and keeps the ladder it was started with | ported verbatim |
| 1473 | and its measured window | ported verbatim |
| 1481 | a session recorded on an auto-selected release resumes on that exact release | ported verbatim |
| 1485 | a restored non-table release collapses the sonnet rung, as every override does | ported verbatim |
| 1486 | and leaves the haiku rung alone | ported verbatim |
| 1487 | with the same assumed ceiling a fresh start gets | ported verbatim |
| 1488 | and the assumption is named on a resume too | ported verbatim |
| 1492 | resolve-model routes a future release to grok | ported verbatim |
| 1494 | resolve-model normalises a future build id | ported verbatim |
| 1510 | the local route lists the last-known model and says so | ported verbatim |
| 1511 | the local route fetches no catalog | ported verbatim |
| 1512 | the native grok CLI is never consulted locally | ported verbatim |
| 1526 | a native CLI that lists a newer model does not move the remote route | ported verbatim |
| 1527 | the native grok CLI is never consulted remotely | ported verbatim |
| 1528 | discovery still costs exactly one request | ported verbatim |
| 1537 | an override wins over the catalog and says so | ported verbatim |
| 1547 | an override to a variant model still execs | ported verbatim |
| 1548 | an override reaches the environment verbatim | ported verbatim |
| 1549 | an unverified override gets the conservative ceiling | ported verbatim |
| 1550 | the unknown ceiling is stated on stderr | ported verbatim |
| 1559 | a malformed override is reported | ported verbatim |
| 1560 | a malformed override leaves the pin in place | ported verbatim |
| 1569 | local exec with an override succeeds | ported verbatim |
| 1570 | the local route honours an explicit override | ported verbatim |
| 1571 | a verified override keeps its real ceiling on the local route | ported verbatim |
| 1578 | the local route lists the deterministic pinned model | ported verbatim |
| 1589 | a tier override execs | ported verbatim |
| 1590 | the tier override reaches the environment | ported verbatim |
| 1591 | a verified tier keeps its own real ceiling | ported verbatim |
| 1592 | a model the table declares is never called unverified | ported verbatim |
| 1602 | pinning the primary stays quiet | ported verbatim |
| 1615 | pinning the astra primary execs | ported verbatim |
| 1616 | the resumed primary is the pinned one | ported verbatim |
| 1617 | pinning the primary leaves every rung on the table value | ported verbatim |
| 1618 | pinning the primary keeps the row ceiling | ported verbatim |
| 1628 | an override to another model still wins | ported verbatim |
| 1629 | the tracking rung collapses onto a real override | ported verbatim |
| 1630 | a rung equal to the replaced primary follows the override | ported verbatim |
| 1633 | a rung pointed elsewhere is never rewritten | ported verbatim |
| 1634 | the override looks its own window up | ported verbatim |
| 1646 | a smaller tier never inherits the row ceiling | ported verbatim |
| 1659 | $unmeasured gets the conservative ceiling | ported verbatim |
| 1660 | and its unknown ceiling is stated | ported verbatim |
| 1661 | and nothing is assumed from a gpt-5.6 predecessor | ported verbatim |
| 1673 | an explicit astra override still selects astra | ported verbatim |
| 1674 | and leaves the smaller Luna rung reachable | ported verbatim |
| 1675 | so its ceiling is capped at the row's, not astra's 900000 | ported verbatim |
| 1676 | and the cap is stated | ported verbatim |
| 1683 | resolve-model resolves a primary | ported verbatim |
| 1684 | a shared-tier agent is named by its primary | ported verbatim |
| 1688 | resolve-model resolves a tier model | ported verbatim |
| 1695 | resolve-model accepts the astra primary | ported verbatim |
| 1696 | resolve-model maps the astra primary to its row | ported verbatim |
| 1698 | the shared haiku rung resolves to luna's row | ported verbatim |
| 1706 | resolve-model refuses $gone rather than remapping it | ported verbatim |
| 1730 | a non-primary rung shared by same-route rows resolves | ported; the shared-tier table is a user `models.tsv` instead of a rewritten copy of the script |
| 1731 | to the first claimant, keeping the tier id | ported; the shared-tier table is a user `models.tsv` instead of a rewritten copy of the script |
| 1733 | a rung claimed by rows on different routes is refused | ported; the shared-tier table is a user `models.tsv` instead of a rewritten copy of the script |
| 1734 | and the refusal says why | ported; the shared-tier table is a user `models.tsv` instead of a rewritten copy of the script |
| 1738 | an xAI build id normalises to the proxy alias | ported verbatim |
| 1742 | an unknown id routes when one agent claims the prefix | ported verbatim |
| 1746 | an unknown gpt id is refused rather than guessed | ported verbatim |
| 1747 | the refusal says why | ported verbatim |
| 1751 | a native model has no agent profile | ported verbatim |
