# Migration coverage: upstream suite → cc-router

Every assertion site in dotfiles' `scripts/test-cc-harness-agents.sh` and where
it lives now. Source: dotfiles `5eecae2` (blob `5526f4f1666d`), the bash
implementation's suite at the equivalence baseline (first mapped at `f429346`;
the deltas are summarized below).

Counts are **assertion call sites**, not executed assertions: loops and
`assert_secret_safe` (three checks) expand at run time. The suite here runs
451 assertions (`tests/run`); upstream's figure included wrapper and parity
checks that stay in dotfiles.

| Disposition | Sites |
|---|---|
| ported verbatim | 262 |
| ported, amended after the cutover (Codex minor discovery) | 7 |
| stays in dotfiles (wrapper, statusline parity, zsh functions) | 58 |
| ported with a harness adaptation (noted per row) | 18 |
| replaced by an equivalent behavioural check | 5 |
| retired (curl-specific, justified per row) | 2 |
| **total** | **352** |

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
here it is a user `models.tsv` (adapted).

**Delta `de7a736` → `5eecae2`** (dotfiles #45, gpt-6-sol/luna as the Codex
Sol/Luna rungs): data — sol/luna primaries and the opus/haiku rungs move to
gpt-6, their floor-tested 372000 joins the verified windows; behaviour — the
retained list keeps the superseded gpt-5.6-sol/luna resumable on their own id
(resolve-model: primary > tier > retained > family, a retained id whose row is
gone is refused before the family fallback), and a retained pin swaps only the
primary and the rungs equal to it. The stale-retained case upstream rewrites
the retained list in a copy of the script; the list is policy in the binary,
so here a user `models.tsv` drops the row instead (adapted). Every upstream
site is mapped.

**Changed here after the cutover** (cc-router, 2026-10-02, Codex minor
discovery): within the verified major 6 every Codex role takes the newest
offered `gpt-6[.n]-<role>` and inherits 372000; an override moves only the
rungs in the replaced primary's class. Rows marked *amended* below assert the
new behaviour; the codex discovery, resume and resolve-model cases were added.

| Upstream line | Assertion | Here |
|---|---|---|
| 318 | remote list succeeds | ported verbatim |
| 319 | remote list probes exactly once | ported verbatim |
| 320 | remote list marks a returned model available | ported verbatim |
| 321 | remote list marks a missing model unavailable | ported verbatim |
| 322 | remote list emits every table row | ported verbatim |
| 323 | a captured listing carries no header line | ported verbatim |
| 338 | the wrapper has a --$agent selector | stays in dotfiles: row ↔ wrapper/statusline parity (wrapper and statusline are not extracted yet) |
| 340 | the wrapper has a --$agent selector | stays in dotfiles: row ↔ wrapper/statusline parity (wrapper and statusline are not extracted yet) |
| 366 | an explicit header is accepted | ported verbatim |
| 367 | the header names all four columns | ported verbatim |
| 368 | the header leaves every agent row intact | ported verbatim |
| 371 | every available column starts at one shared offset | ported verbatim |
| 372 | short names are padded to the widest row | ported verbatim |
| 379 | an explicit no-header is accepted | ported verbatim |
| 380 | no-header keeps the local listing at one line per agent | ported verbatim |
| 386 | an unknown list flag is a usage error | ported verbatim |
| 387 | usage documents the header flags | ported verbatim |
| 402 | the default captured listing succeeds | ported verbatim |
| 403 | the default captured listing starts with an agent row | ported verbatim |
| 404 | remote probe uses the fixed models URL | replaced: the test gateway asserts `GET /c/<case>/v1/models` on the configured gateway |
| 405 | remote probe disables redirects | replaced: redirect case (302 reported, target never requested) in the suite + TestProbeClassifiesFailures |
| 406 | remote probe caps the response body at 1 MiB | replaced: 1 MiB body-cap case in the suite + TestProbeClassifiesFailures (incl. chunked) |
| 407 | remote probe restricts the protocol to HTTPS | replaced: plaintext URL refused with exit 3 and no request (suite) + TestSettingValidation |
| 408 | remote probe tells curl to read headers from stdin | retired: curl-specific transport (`--header @-`); the Go probe builds headers in memory and spawns no process. Security intent kept by the test gateway header assertions and TestProductionRemoteRoute (no secret in diagnostics) |
| 409 | remote probe sends bearer authentication | ported (reads the test gateway request log) |
| 410 | remote probe sends the Access client ID | ported (test-gateway log; header name canonicalized by the server) |
| 411 | remote probe sends the Access client secret | ported (test-gateway log; header name canonicalized by the server) |
| 412 | curl argv hides probe secrets | retired: there is no curl argv; no child process exists in the probe |
| 413 | remote probe never enables redirect following | replaced: redirect case (never followed) |
| 414 | remote list stderr hides secrets | ported verbatim |
| 426 | explicit profile override succeeds | ported verbatim |
| 427 | explicit profile resolves its API key indirectly | ported verbatim |
| 428 | explicit profile resolves its Access ID indirectly | ported verbatim |
| 429 | explicit profile secret is absent from stderr | ported verbatim |
| 445 | remote exec succeeds | ported verbatim |
| 446 | remote exec probes exactly once | ported verbatim |
| 447 | remote exec exports the fixed HTTPS base URL | ported; asserts the configured test-gateway URL instead of the personal host |
| 448 | remote exec exports the selected API key | ported verbatim |
| 449 | remote exec identifies the route | ported verbatim |
| 450 | remote exec preserves NO_PROXY | ported verbatim |
| 451 | remote exec preserves no_proxy | ported verbatim |
| 452 | remote header merge preserves order and canonicalizes Access headers | ported verbatim |
| 453 | remote header merge removes inherited Access ID | ported verbatim |
| 454 | remote header merge removes inherited Access secret | ported verbatim |
| 455 | remote exec clears provider selectors | ported verbatim |
| 456 | remote exec clears the gateway selector | ported verbatim |
| 457 | remote exec clears the Anthropic Google selector | ported verbatim |
| 458 | remote exec exports the primary model | ported verbatim |
| 459 | remote exec preserves target argv | ported verbatim |
| 460 | remote exec stderr hides secrets | ported verbatim |
| 483 | remote headers reject $header_case input | ported verbatim |
| 484 | remote header $header_case error hides secrets | ported verbatim |
| 493 | local list succeeds | ported verbatim |
| 494 | local list uses only the reachability probe | ported verbatim |
| 495 | local list keeps provider availability checks | amended: asserted on terra (sol now carries the last-known label) |
| 506 | local exec succeeds | ported verbatim |
| 507 | local exec uses only the reachability probe | ported verbatim |
| 508 | local exec exports the loopback gateway | ported; asserts the test gateway port instead of the fixed 8317 |
| 509 | local exec exports the local token | ported verbatim |
| 510 | local exec identifies the route | ported verbatim |
| 511 | loopback bypass is added only locally | ported verbatim |
| 512 | local lowercase bypass matches the merged list | ported verbatim |
| 513 | local exec preserves non-Access custom headers | ported verbatim |
| 514 | local exec removes every inherited Access ID | ported verbatim |
| 515 | local exec removes every inherited Access secret | ported verbatim |
| 516 | local exec clears the gateway selector | ported verbatim |
| 517 | local exec clears the Anthropic Google selector | ported verbatim |
| 524 | local exec rejects an unreachable loopback gateway | ported verbatim |
| 525 | local reachability error names the local gateway | ported; asserts the test gateway port instead of the fixed 8317 |
| 526 | local reachability failure never invokes curl | ported verbatim |
| 542 | local headers reject $header_case input | ported verbatim |
| 553 | local list still exits 0 without a fallback marker | ported verbatim |
| 554 | a missing marker still lists every row | ported verbatim |
| 555 | a missing marker marks every row unavailable | ported verbatim |
| 556 | a blocked local listing still labels the last-known model | ported verbatim |
| 557 | and stays at four columns | ported verbatim |
| 558 | the missing marker note names the fix | ported; generic default hint (personal `--mars-stopped` flag is configuration now, see TestConfigFile) |
| 566 | local exec refuses without a fallback marker | ported verbatim |
| 567 | the exec refusal names the fix | ported; generic default hint |
| 575 | local exec refuses an expired fallback marker | ported verbatim |
| 576 | the expired marker is named as expired | ported verbatim |
| 577 | the expired marker names the fix | ported; generic default hint |
| 585 | local list reports an expired marker per row | ported verbatim |
| 594 | local exec refuses a corrupt fallback marker | ported verbatim |
| 595 | a corrupt marker is reported as unusable | ported verbatim |
| 604 | the marker gate precedes the token and gateway probes | ported verbatim |
| 605 | the marker reason wins over the token reason | ported verbatim |
| 606 | the token probe never ran | ported verbatim |
| 607 | the gateway probe never ran | ported verbatim |
| 616 | a prepared fallback still requires the gateway token | ported verbatim |
| 617 | the token probe runs once the marker is valid | ported verbatim |
| 638 | remote probe failure exits 1 ($expected) | ported verbatim |
| 639 | remote probe classifies $expected | ported verbatim |
| 640 | failed remote exec still probes exactly once ($expected) | ported; request count observed by the test gateway — 0 for the two transport failures (the request never arrives; for TLS the secrets never reach the peer) |
| 641 | remote $expected error hides secrets | ported verbatim |
| 653 | remote exec rejects a missing requested model | ported verbatim |
| 654 | missing-model error names only the model | ported verbatim |
| 655 | missing-model stderr hides secrets | ported verbatim |
| 666 | remote list completes when a tier model is missing | ported verbatim |
| 670 | remote list validates the shared haiku rung (${agent_model%%:*}) | ported verbatim |
| 672 | missing Codex tier does not affect unrelated rows | ported verbatim |
| 673 | missing-tier stderr hides secrets | ported verbatim |
| 690 | remote list succeeds with a full catalog | ported verbatim |
| 691 | a complete catalog makes astra available | ported verbatim |
| 711 | $agent execs against a catalog without spark | ported verbatim |
| 712 | $agent exports its own primary | ported verbatim |
| 713 | $agent exports the shared Astra/Sol/Terra/Luna ladder | ported verbatim |
| 714 | $agent exports the smallest rung's window | ported verbatim |
| 715 | $agent execs without a note (nothing selected, nothing assumed) | ported verbatim |
| 732 | a catalog with gpt-6 variants still execs sol | ported verbatim |
| 733 | catalog-only variants move no codex rung | amended: variants and a 7.x move no codex rung (gpt-6.1-sol now does) |
| 734 | catalog-only variants leave the ceiling alone | amended: variants and a 7.x leave the ceiling alone |
| 749 | a pinned superseded sol still execs while served | ported verbatim |
| 750 | the superseded pin is not upgraded | ported verbatim |
| 753 | a retained pin swaps the primary and keeps the other rungs | ported verbatim |
| 754 | the superseded pin keeps its measured window | ported verbatim |
| 755 | and nothing about it is unknown | ported verbatim |
| 766 | a pinned superseded sol the route dropped is unavailable | ported verbatim |
| 767 | and the dropped id is named, not remapped | ported verbatim |
| 781 | a missing Terra family makes even astra unavailable | ported verbatim |
| 782 | the missing family is named | ported verbatim |
| 796 | remote list completes when astra is absent from the catalog | ported verbatim |
| 797 | an absent astra primary marks its row unavailable | ported verbatim |
| 798 | an absent astra takes the sol row's fable rung with it | ported verbatim |
| 799 | an absent astra leaves unrelated rows alone | ported verbatim |
| 808 | invalid profile syntax is a capability error | ported verbatim |
| 809 | invalid profile syntax has a safe diagnostic | ported verbatim |
| 819 | control characters in remote secrets are rejected | ported verbatim |
| 820 | control-character error is generic | ported verbatim |
| 821 | secret-validation stderr hides secrets | ported verbatim |
| 831 | empty remote credentials are rejected | ported verbatim |
| 832 | empty-credential error is generic | ported verbatim |
| 833 | empty-credential stderr hides other secrets | ported verbatim |
| 897 | zsh wrapper routes foreign models remotely by default | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 898 | zsh remote prefix omits --local | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 899 | zsh remote route preserves prompt argv | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 902 | zsh wrapper accepts --local with a foreign model | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 903 | zsh local prefix delegates route selection to helper | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 906 | zsh wrapper treats --local after bare -- as passthrough | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 907 | passthrough --local does not select local routing | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 908 | passthrough --local reaches Claude argv | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 911 | zsh wrapper rejects --local without a foreign model | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 912 | zsh wrapper explains bare --local rejection | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 915 | zsh wrapper keeps native-model conflict detection | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 916 | zsh wrapper reports native-model conflict | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 919 | zsh wrapper keeps foreign-model conflict detection | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 920 | zsh wrapper reports foreign-model conflict | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 931 | zsh wrapper accepts --astra | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 932 | --astra hands the row name straight to the helper | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 933 | the selector is consumed, never leaked into Claude argv | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 934 | --astra preserves the prompt argv | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 940 | a plain launch still succeeds | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 941 | a plain launch never reaches the routing helper | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 949 | zsh wrapper accepts --$native_case | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 951 | --$native_case expands to a native --model | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 953 | the shorthand itself never reaches Claude argv | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 954 | --$native_case is native, so it never routes | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 958 | a native shorthand conflicts with a foreign flag | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 961 | the conflict names the shorthand, not --model | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 962 | a shorthand conflict never blames a bare --model | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 965 | a bare --model still conflicts with a foreign flag | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 966 | a bare --model is still named as itself | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 969 | a native shorthand conflicts with an explicit --model | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 970 | the shorthand/--model conflict is reported | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 973 | the conflict is caught in either order | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 976 | two different native shorthands conflict | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 977 | the native/native conflict is reported | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 980 | repeating the same native shorthand is not a conflict | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 981 | a repeated shorthand is only expanded once | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 984 | a literal --opus after bare -- is passthrough | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 985 | passthrough --opus reaches Claude argv | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 997 | zsh wrapper script path succeeds | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 999 | zsh wrapper pins the production system script path | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 1001 | zsh wrapper pins the production system script path | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 1003 | isolated resume test uses its deterministic script fake | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 1005 | zsh wrapper never executes PATH script | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 1007 | zsh wrapper never executes PATH script | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 1009 | resume history preserves yolo, local route, and model | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 1033 | $zsh_function_rel exists | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 1035 | .zshrc sources $zsh_function_rel | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 1037 | .zshrc sources $zsh_function_rel | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 1043 | .zshrc no longer defines $zsh_function_name() itself | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 1045 | .zshrc no longer defines $zsh_function_name() itself | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 1056 | the function files load in a bare zsh | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 1057 | listening.zsh defines listening | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 1058 | claude.zsh defines claude | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 1059 | sourcing the function files stays silent | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 1065 | listening rejects more than one argument | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 1066 | listening explains its usage | stays in dotfiles: zsh wrapper / .zshrc / listening.zsh |
| 1111 | discovery over the live catalog succeeds | ported verbatim |
| 1112 | the live catalog selects grok-4.6, its newest candidate, with no note | ported verbatim |
| 1113 | no 4.20 variant is ever selected | ported verbatim |
| 1114 | reasoning and non-reasoning variants stay out of the selection | ported verbatim |
| 1115 | multi-agent variants stay out of the selection | ported verbatim |
| 1116 | a superseded major version is never selected | ported verbatim |
| 1126 | exec over the live catalog succeeds | ported verbatim |
| 1127 | the live ladder puts the newest model on the primary | ported verbatim |
| 1128 | the live ladder puts the newest model on the opus tier | ported verbatim |
| 1129 | the live ladder puts the predecessor on the sonnet tier | ported verbatim |
| 1130 | the haiku tier stays on composer | ported verbatim |
| 1140 | exec with a single canonical model succeeds | ported verbatim |
| 1141 | a lone canonical model is the primary | ported verbatim |
| 1142 | the sonnet tier mirrors the primary when nothing older exists | ported verbatim |
| 1152 | an override sets the primary | ported verbatim |
| 1153 | an override sets the opus tier | ported verbatim |
| 1154 | an override collapses the sonnet tier onto the same model | ported verbatim |
| 1155 | an override still leaves the haiku tier alone | ported verbatim |
| 1165 | a future 4.x is selected unedited, with candidates and the assumed window named | ported verbatim |
| 1167 | a selection note keeps the row at four columns | ported verbatim |
| 1174 | exec with a future release succeeds | ported verbatim |
| 1175 | exec exports exactly the model list reported for the same catalog | ported verbatim |
| 1176 | the subagent model follows the selection | ported verbatim |
| 1177 | the opus tier follows the selection | ported verbatim |
| 1178 | the sonnet tier takes the rung below the new primary | ported verbatim |
| 1179 | a tier pointed elsewhere never follows the selection | ported verbatim |
| 1180 | a future release is given its predecessor's window | ported verbatim |
| 1181 | the selection note never leaks into the target argv | ported verbatim |
| 1182 | exec names the automatic selection on stderr | ported verbatim |
| 1183 | exec names the assumption on stderr, every run | ported verbatim |
| 1184 | the selection note carries no secret | ported verbatim |
| 1193 | 5.0 outranks every 4.x | ported verbatim |
| 1194 | 4.20 outranks 4.9 by component, not by decimal value | ported verbatim |
| 1201 | 4.20 is the newest of 4.6, 4.9 and 4.20 | ported verbatim |
| 1202 | candidates are listed newest first | ported verbatim |
| 1215 | bare majors, other majors, patch ids, variants and malformed ids are never selected | ported verbatim |
| 1216 | and none of them is reported as a candidate | ported verbatim |
| 1226 | an overlong version component does not abort the listing | ported verbatim |
| 1227 | an overlong version component is not a candidate | ported verbatim |
| 1235 | a zero-padded minor does not abort the listing | ported verbatim |
| 1236 | a zero-padded minor compares as a decimal number | ported verbatim |
| 1255 | two unknown releases: the newest is selected | ported verbatim |
| 1256 | an assumption never chains from another assumption | ported verbatim |
| 1257 | a rung with the same assumed window holds the ceiling | ported verbatim |
| 1266 | a smaller catalog limit is respected, not overridden by the assumption | ported verbatim |
| 1267 | an explicit limit is not reported as an assumption | ported verbatim |
| 1268 | a larger rung still holds the smaller ceiling | ported verbatim |
| 1277 | the first release of a new major is selected | ported verbatim |
| 1278 | and gets at least the window of the newest verified 4.x | ported verbatim |
| 1279 | named as an assumption across the major boundary too | ported verbatim |
| 1280 | the verified 4.x rung holds the assumed ceiling | ported verbatim |
| 1290 | a release below every verified one has no predecessor to assume from | ported verbatim |
| 1298 | a variant never inherits a canonical release's window | ported verbatim |
| 1306 | grok-4.7 is selected from the catalog | ported verbatim |
| 1307 | with a documented window, not an assumed one | ported verbatim |
| 1318 | a release with catalog metadata is selected | ported verbatim |
| 1319 | catalog metadata supplies the ceiling | ported verbatim |
| 1320 | a rung that holds the announced ceiling is kept | ported verbatim |
| 1321 | a known window is not called unknown | ported verbatim |
| 1322 | an advertised ceiling is named as advertised | ported verbatim |
| 1333 | context_length $float_ctx does not fail the probe | ported verbatim |
| 1334 | an integral $float_ctx is read as 300000, never inflated to the cap | ported verbatim |
| 1335 | context_length $float_ctx never reaches a shell comparison raw | ported verbatim |
| 1344 | an advertised window is clamped to the largest measured one | ported verbatim |
| 1355 | a measured window outranks catalog metadata | ported verbatim |
| 1365 | the primary's window is announced | ported verbatim |
| 1366 | a rung with a smaller window is skipped rather than overflowed | ported verbatim |
| 1379 | context_length $bad_ctx does not fail the probe | ported verbatim |
| 1380 | context_length $bad_ctx leaves the window unknown | ported verbatim |
| 1392 | a separator-bearing id does not displace the real candidate | ported verbatim |
| 1393 | a separator-bearing id cannot forge another id's window | ported verbatim |
| 1401 | a variant's advertised window is never believed | ported verbatim |
| 1409 | an agent without discovery never reads catalog metadata | ported verbatim |
| 1420 | a valid catalog below the last-known model selects its newest candidate | ported verbatim |
| 1428 | exec follows a catalog below the last-known model | ported verbatim |
| 1429 | exec exports what the catalog offers, not the stale last-known model | ported verbatim |
| 1430 | the rung below follows downwards too | ported verbatim |
| 1431 | a measured older release keeps its measured window | ported verbatim |
| 1441 | no eligible candidate: unavailable, nothing substituted, fallback labelled | ported verbatim |
| 1449 | exec refuses when the catalog offers no candidate | ported verbatim |
| 1450 | nothing is exec'd on a last-known model the route does not offer | ported verbatim |
| 1451 | the refusal names the fallback for what it is | ported verbatim |
| 1460 | an unavailable catalog labels the last-known model | ported verbatim |
| 1461 | a row without discovery carries no fallback label | amended: terra (primary outside the pattern); sol is labelled |
| 1462 | the label is not sprayed over rows that never discover | amended: asserted on terra |
| 1469 | an unparsable catalog is stale, not empty | ported verbatim |
| 1476 | exec never runs on a stale catalog | ported verbatim |
| 1477 | no model is exported without a catalog on the remote route | ported verbatim |
| 1486 | a missing haiku tier keeps the row unavailable after discovery | ported verbatim |
| 1496 | a pin the catalog lacks is reported missing, never swapped for the latest | ported verbatim |
| 1506 | a session recorded on grok-4.6 resumes on grok-4.6 while 4.7 is offered | ported verbatim |
| 1507 | and keeps the ladder it was started with | ported verbatim |
| 1508 | and its measured window | ported verbatim |
| 1516 | a session recorded on an auto-selected release resumes on that exact release | ported verbatim |
| 1520 | a restored non-table release collapses the sonnet rung, as every override does | ported verbatim |
| 1521 | and leaves the haiku rung alone | ported verbatim |
| 1522 | with the same assumed ceiling a fresh start gets | ported verbatim |
| 1523 | and the assumption is named on a resume too | ported verbatim |
| 1527 | resolve-model routes a future release to grok | ported verbatim |
| 1529 | resolve-model normalises a future build id | ported verbatim |
| 1545 | the local route lists the last-known model and says so | ported verbatim |
| 1546 | the local route fetches no catalog | ported verbatim |
| 1547 | the native grok CLI is never consulted locally | ported verbatim |
| 1561 | a native CLI that lists a newer model does not move the remote route | ported verbatim |
| 1562 | the native grok CLI is never consulted remotely | ported verbatim |
| 1563 | discovery still costs exactly one request | ported verbatim |
| 1572 | an override wins over the catalog and says so | ported verbatim |
| 1582 | an override to a variant model still execs | ported verbatim |
| 1583 | an override reaches the environment verbatim | ported verbatim |
| 1584 | an unverified override gets the conservative ceiling | ported verbatim |
| 1585 | the unknown ceiling is stated on stderr | ported verbatim |
| 1594 | a malformed override is reported | ported verbatim |
| 1595 | a malformed override leaves the pin in place | ported verbatim |
| 1604 | local exec with an override succeeds | ported verbatim |
| 1605 | the local route honours an explicit override | ported verbatim |
| 1606 | a verified override keeps its real ceiling on the local route | ported verbatim |
| 1613 | the local route lists the deterministic pinned model | ported verbatim |
| 1624 | a tier override execs | ported verbatim |
| 1625 | the tier override reaches the environment | ported verbatim |
| 1626 | a verified tier keeps its own real ceiling | ported verbatim |
| 1627 | a model the table declares is never called unverified | ported verbatim |
| 1637 | pinning the primary stays quiet | ported verbatim |
| 1650 | pinning the astra primary execs | ported verbatim |
| 1651 | the resumed primary is the pinned one | ported verbatim |
| 1652 | pinning the primary leaves every rung on the table value | ported verbatim |
| 1653 | pinning the primary keeps the row ceiling | ported verbatim |
| 1663 | an override to another model still wins | ported verbatim |
| 1664 | the tracking rung collapses onto a real override | amended: the Terra rung is another class and stays |
| 1665 | a rung equal to the replaced primary follows the override | ported verbatim |
| 1668 | a rung pointed elsewhere is never rewritten | ported verbatim |
| 1669 | the override looks its own window up | ported verbatim |
| 1681 | a smaller tier never inherits the row ceiling | ported verbatim |
| 1692 | gpt-5.5 gets the conservative ceiling | ported verbatim |
| 1693 | and its unknown ceiling is stated | ported verbatim |
| 1694 | and nothing is assumed from a gpt-5.6 predecessor | ported verbatim |
| 1704 | a pinned gpt-6-luna keeps its floor-tested budget | ported verbatim |
| 1705 | and its budget is not reported unknown | ported verbatim |
| 1716 | an explicit astra override still selects astra | ported verbatim |
| 1717 | and leaves the smaller Luna rung reachable | ported verbatim |
| 1718 | so its ceiling is capped at the row's, not astra's 900000 | ported verbatim |
| 1719 | and the cap is stated | ported verbatim |
| 1726 | resolve-model resolves a primary | ported verbatim |
| 1727 | a shared-tier agent is named by its primary | ported verbatim |
| 1731 | resolve-model resolves a tier model | ported verbatim |
| 1738 | resolve-model accepts the astra primary | ported verbatim |
| 1739 | resolve-model maps the astra primary to its row | ported verbatim |
| 1741 | the shared haiku rung resolves to luna's row | ported verbatim |
| 1749 | resolve-model keeps ${retained#*:} resumable | ported verbatim |
| 1750 | ${retained#*:} resolves to its former row, unrenamed | ported verbatim |
| 1759 | resolve-model refuses $gone rather than remapping it | amended: gpt-7-sol and a variant replace gpt-6-terra/gpt-6.1-sol, which now resolve |
| 1783 | a non-primary rung shared by same-route rows resolves | ported; the shared-tier table is a user `models.tsv` instead of a rewritten copy of the script |
| 1784 | to the first claimant, keeping the tier id | ported; the shared-tier table is a user `models.tsv` instead of a rewritten copy of the script |
| 1786 | a rung claimed by rows on different routes is refused | ported; the shared-tier table is a user `models.tsv` instead of a rewritten copy of the script |
| 1787 | and the refusal says why | ported; the shared-tier table is a user `models.tsv` instead of a rewritten copy of the script |
| 1804 | a retained id naming a removed row is refused | ported; the retained list is policy in the binary, so a user `models.tsv` drops the row instead of a rewritten copy of the script |
| 1808 | a stale retained id is not handed to a single-owner family | ported; the retained list is policy in the binary, so a user `models.tsv` drops the row instead of a rewritten copy of the script |
| 1809 | and the refusal names the missing row | ported; the retained list is policy in the binary, so a user `models.tsv` drops the row instead of a rewritten copy of the script |
| 1811 | while a retained id with a live row still resolves | ported; the retained list is policy in the binary, so a user `models.tsv` drops the row instead of a rewritten copy of the script |
| 1815 | an xAI build id normalises to the proxy alias | ported verbatim |
| 1819 | an unknown id routes when one agent claims the prefix | ported verbatim |
| 1823 | an unknown gpt id is refused rather than guessed | ported verbatim |
| 1824 | the refusal says why | ported verbatim |
| 1828 | a native model has no agent profile | ported verbatim |
