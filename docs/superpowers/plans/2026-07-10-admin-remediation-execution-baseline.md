# Admin Remediation Execution Baseline

Baseline SHA: 9bf7608aa724b526e4034a8982f41b67eca3c5cb
Recorded UTC: 2026-07-11T07:08:36Z
Starting status SHA-256: 39a5ff04efed2f5cae5f612cfe8fb05c4efb28f993b28db784f5536cb128a88c

## Starting Status

```text
 M .github/ISSUE_TEMPLATE/bug_report.md
 M .github/workflows/docker-publish.yml
 M .github/workflows/release.yml
 M .goreleaser.yaml
 M CHANGELOG.md
 M CLAUDE.md
 M CONTEXT.md
 M CONTRIBUTING.md
 M DESIGN.md
 M Dockerfile
 M Formula/depsilo.rb
 M README.md
 M RELEASE_CHECKLIST.md
 M SECURITY.md
 M config.example.toml
 M docs/DIRECTION.md
 M docs/README_zh.md
 M docs/agents/domain.md
 M docs/self-test-checklist.md
 M docs/superpowers/plans/2026-07-10-tamper-detection.md
 M docs/superpowers/specs/2026-07-10-tamper-detection-design.md
 M install.sh
 M internal/adapter/npm/rewriter.go
 M internal/api/public/discover.go
 M internal/api/public/mcp.go
 M internal/api/router.go
 M internal/cli/initagent.go
 M internal/cli/prompt.go
 M internal/cli/status.go
 M internal/config/config.go
 M internal/config/loader_test.go
 M internal/db/blocklist.go
 M internal/entitlement/checker.go
 M internal/license/license_test.go
 M internal/prompts/integration.md
 M internal/prompts/prompts.go
 M internal/prompts/prompts_test.go
 M internal/quarantine/allowlist.go
 M internal/quarantine/policy.go
 M internal/quarantine/quarantine_test.go
 M internal/quarantine/resolvers/resolver.go
 M internal/server/server.go
 M internal/tray/tray.go
 M tests/integration/license_trial_test.go
 M web/src/admin/pages/Settings.tsx
 M web/src/i18n/en.ts
 M web/src/i18n/zh.ts
 M web/src/lib/api.ts
 M web/src/lib/ecosystemData.ts
 D web/src/portal/components/AllInOnePane.tsx
 M web/src/portal/components/ConfigurePane.tsx
 M web/src/portal/components/IntegrationPromptButton.tsx
 D web/src/portal/components/LanguageRail.tsx
?? internal/cli/status_test.go
```

## Dirty Paths

<!-- DIRTY_PATHS_BEGIN -->
.github/ISSUE_TEMPLATE/bug_report.md
.github/workflows/docker-publish.yml
.github/workflows/release.yml
.goreleaser.yaml
CHANGELOG.md
CLAUDE.md
CONTEXT.md
CONTRIBUTING.md
DESIGN.md
Dockerfile
Formula/depsilo.rb
README.md
RELEASE_CHECKLIST.md
SECURITY.md
config.example.toml
docs/DIRECTION.md
docs/README_zh.md
docs/agents/domain.md
docs/self-test-checklist.md
docs/superpowers/plans/2026-07-10-tamper-detection.md
docs/superpowers/specs/2026-07-10-tamper-detection-design.md
install.sh
internal/adapter/npm/rewriter.go
internal/api/public/discover.go
internal/api/public/mcp.go
internal/api/router.go
internal/cli/initagent.go
internal/cli/prompt.go
internal/cli/status.go
internal/cli/status_test.go
internal/config/config.go
internal/config/loader_test.go
internal/db/blocklist.go
internal/entitlement/checker.go
internal/license/license_test.go
internal/prompts/integration.md
internal/prompts/prompts.go
internal/prompts/prompts_test.go
internal/quarantine/allowlist.go
internal/quarantine/policy.go
internal/quarantine/quarantine_test.go
internal/quarantine/resolvers/resolver.go
internal/server/server.go
internal/tray/tray.go
tests/integration/license_trial_test.go
web/src/admin/pages/Settings.tsx
web/src/i18n/en.ts
web/src/i18n/zh.ts
web/src/lib/api.ts
web/src/lib/ecosystemData.ts
web/src/portal/components/AllInOnePane.tsx
web/src/portal/components/ConfigurePane.tsx
web/src/portal/components/IntegrationPromptButton.tsx
web/src/portal/components/LanguageRail.tsx
<!-- DIRTY_PATHS_END -->

## Dirty Evidence

Fields are tab-separated: kind, status, content SHA-256, normalized diff SHA-256, diff summary, path.

```text
<!-- DIRTY_EVIDENCE_BEGIN -->
tracked	 M	35117ffe1dd55a2aa2bb6a647f648f2e5ad1ad0229931f56fea2d9327dcd313c	ddb7fd26f1b85e4d7a4052d5ed70fcbac24f13fb7edaebc294b2b3d155aa24a1	add=2,del=2;	.github/ISSUE_TEMPLATE/bug_report.md
tracked	 M	9402d5f1124bb8cd7ab23c2ff71075ec8831b4de2be7832211b1eeb02f8e04d8	cf9d330e4c4b4aba27d798c8da78bdb34a3bf8d6b0d1aa40474af9734168967e	add=2,del=2;	.github/workflows/docker-publish.yml
tracked	 M	a7bdc4dc5924ef4e5acc0238a14ad43ca75530ab9d3c70c8f76e967ba956d206	7debedcde329513154c400429e254786b04be1deb8f80c73b4d0a4cb09564fd1	add=1,del=1;	.github/workflows/release.yml
tracked	 M	36d3e6cf66a44c59c272ffb3656efa2e3f74e695ff2eaed793f16c0450185295	5e313ba6febfe79e6feeef00b6770ca3c505fc8740b0c7fec489ed5f4925f684	add=1,del=1;	.goreleaser.yaml
tracked	 M	ef3b91b06067b4fd2b02c4be7fee7adbf8decac8164258058eee6c72666a2b9e	965d1ffb8ff9134e9e5c7f02d52b1a9d36835f6341e159b0b3b437035ebf1dd1	add=19,del=17;	CHANGELOG.md
tracked	 M	7f513fee0d7a23587767ba2e7bee87c3360d0b1e8d69236e6ff9d7a8b7a8d934	ffc9393d059d897cfab1986f649ab675a67ef685744585a0bb4bc48f569dae79	add=102,del=113;	CLAUDE.md
tracked	 M	07863960578a6c9c1bfd032db4dfd17d67ce31d1e42e369172734f9d98896b30	f6c0bfec3da5e64b2b073ca385b1d19772d9d75955d3a4ec31da4070f23dce39	add=14,del=10;	CONTEXT.md
tracked	 M	8a35f0f4d45dd8e674d968a6d59269db112295a12c1ac3ed1fbecd258e0e411d	a5c9e1b023dfdcf34e59067e2f2a47ab74f8f54e7bac4010e7ca5cd3da61564f	add=36,del=24;	CONTRIBUTING.md
tracked	 M	1a97406c20db18593d32c72ceab28675a9ede283011637a492b27ec4b64fb02f	617dea4c7f1b8957830b5607d3bc7b898dc3192bad5ff56f01c8379f851af2c0	add=124,del=529;	DESIGN.md
tracked	 M	be4d99cacdbdeed6c4e7ff35f79785aa558a51fef1e66cc35b8429940bee8eb3	27cffad2a674784d3e2fa22f588812ad8d8a93d0f78f4cbfef2459ea80be6a6c	add=1,del=1;	Dockerfile
tracked	 M	e8b584d38873cb5fd88b6c651f44ea2b3455e199acc3e20aa0d21ed4d94189c7	bcde2d7aca949818f4ec1522c2d202f2f3d9eed6f28a3a64619e529758be0fc4	add=5,del=7;	Formula/depsilo.rb
tracked	 M	0dc61300537914444f9142c3dd755b7e821b5b8ca1255ebc63f544efe7d3f32d	616d1a0e04e219443122f8a563c2a98f9074cc5dda462109007322dd2764ac86	add=119,del=54;	README.md
tracked	 M	c95ea2aa0494f3f911b2721899fecfa0f16401a41499fd54f33e73e110ed78ac	efb1cce98fc13db5f78b983bee07b778101463ac3521db1394277cc7b54f2583	add=17,del=6;	RELEASE_CHECKLIST.md
tracked	 M	4c9da57b1e2527cacf41810b643dbc564a49cbc1d04d407cf00f9ec2b5b7f3be	9b1df4bf76c6b2f06c7307405c8d4508866b274210a3c74f1f8c3d555f8e9009	add=12,del=5;	SECURITY.md
tracked	 M	de56619bb76accd369e021560618451639e42de0dc8f0465c655921890e259f0	22946145244e5ba6aead1e8a3ca09d22577708624953ab0a564b0e9757708263	add=24,del=21;	config.example.toml
tracked	 M	78d128574a8c2bd34903ff4a46f00d6a96c71c6ac5650b636d110ad9c8cc3386	7dea1f6827ebe7fc45d00bce1b68b86144d23b184cf1172e59941adf6b300edb	add=99,del=79;	docs/DIRECTION.md
tracked	 M	68cc3d9af7fe9e3da58032ad3c460ca7e87c3c648f60fbcbd2ac039548140473	fb57bca28c8fcd42b6a37f7a127d0b5f4efcb18e41ff7d61da665bb057864dd9	add=60,del=60;	docs/README_zh.md
tracked	 M	2080ffc780bc67772b5eb745c9b58eb8212f900a70e659df2d01e2a280aa57ec	5f637dcf69e8956cd730cdaf19e4967155b93b6a036c89982a62746ea32205df	add=10,del=4;	docs/agents/domain.md
tracked	 M	27e855d0f9e60c3d8b05d54f47fa945ba83f86c2ec60c7c18b2f938d2f69ef79	fd5b8b1843000f0fbebc259349cc007e7cdf8aa1902acbf83bcf78acd83f0343	add=155,del=156;	docs/self-test-checklist.md
tracked	 M	bef5e831e28f2a5c1b50675a89d6cedba305e101f4e0b0ac9a5e90864dbdcfe4	fcf3b1c357b238dee6c80c8745431a16e62d044f82060e7f507a694096dc4a57	add=15,del=6;	docs/superpowers/plans/2026-07-10-tamper-detection.md
tracked	 M	7d4ccb699b265fd8baded746a394685e4982d383ccb3bf71f71bd6f868c58f9c	8d58294c738f92776d81373d2d103fa402d9fe61832a72c3d82d0da5981cbe6e	add=28,del=26;	docs/superpowers/specs/2026-07-10-tamper-detection-design.md
tracked	 M	e93ae6a9a6d569103e47badfaad53e3f33945cf1edb6841cd2bea6e26c810174	b17729c37076e1211381608ab26f0312388af5da6b537f40baac88eb41d0fedc	add=1,del=1;	install.sh
tracked	 M	b853b1fdeab72a0673743ed914bf36d8d4fab1da23712a6bb47949fabe196944	7805e8e9aea0c171b9cccabd2f747a7df291edba89112d1b18f5304e998388af	add=1,del=1;	internal/adapter/npm/rewriter.go
tracked	 M	8a613f28a4f74e63512eed2dc58cc23ae7f99bd7c0a1300c4d3fb8daa726f009	f972409210941ed254c79f40f437faf98327672a54741f4b1cf127be843ba294	add=34,del=28;	internal/api/public/discover.go
tracked	 M	4a2b2a94a9d7043ba5877218f7bc19961070cb6cb496632883313865d123e649	209b71bb3f80be8ff1f4900aff73942616ef51cf7478045e6931cbf664052c9e	add=35,del=35;	internal/api/public/mcp.go
tracked	 M	76d325868d0b69ce8ba337b33fe746bd04a781f52ad2547101ce9282c043187b	9aa589235afec63df84918c4cdb8933e22c54255933806af2446791cbd0c8e59	add=9,del=9;	internal/api/router.go
tracked	 M	0beb8e59ffef40123f62b21bc62bf98cafe1e8ecc8b81b3c32e62bafde07ad15	5a50171c5d20c85a87cdafee2ea462270330f1fbafdce94c60ee9b0c30b1fcea	add=5,del=3;	internal/cli/initagent.go
tracked	 M	e9bcfd7cb240b4df55e3973e4b15b8bac0f98f5c9a55a0f2335b8f9a9aa10d7e	9793ac58fced44c06c6f28e527b5cc66be7c537716958f8c4dcdb5408fd4722f	add=4,del=3;	internal/cli/prompt.go
tracked	 M	ededcdd3e2532c2e8c3d615f6ca2b18742c51d2a905eccc2db9b28c3ee5ec372	32e87278ebd6639e05e480b2abeeee26530297937f7d53239a8c586c73a59293	add=12,del=3;	internal/cli/status.go
untracked	??	ec2ebc7ad789777c9ecf0e49a5fa4e14fc202e64e9c173a02450999dce700376	ec2ebc7ad789777c9ecf0e49a5fa4e14fc202e64e9c173a02450999dce700376	untracked-bytes=773	internal/cli/status_test.go
tracked	 M	1c204c92639d22dc1ef179a2b40d4bd889e877f23bab5e0269c413b99c48764f	86cffe22140692d8c94da2b974c888fd34af83106830826f49251e5ffa579fce	add=8,del=8;	internal/config/config.go
tracked	 M	9b1dae07f55e3592ec393d678adcfbe8f412b49237c018ae9102603c2b4b05ec	e6b15aefede233091d92ef27acfa59cea7a6fde86295490473a1fe467b7d00ea	add=32,del=0;	internal/config/loader_test.go
tracked	 M	75ebd9930672529a4849576571a9fe35f704f8bf68bf284e904b088650ab4aca	e5ca8f52ac0802f66496a345897b37b432100cf1a0fb9ef172c01cfa7b46217c	add=2,del=2;	internal/db/blocklist.go
tracked	 M	d835fc279eb9b33fe2e597f1f1f04586137f1f169b287bbb2ee029231ced9954	c6d26238a1971d7e9566cc8211eaf8ded2b07c474cd2e9a82f533d088d0d7951	add=1,del=1;	internal/entitlement/checker.go
tracked	 M	91a414015d98864dd88a0d1d64769e6aa57de653ed3d53b99ecab6042eef8cf5	1051cf343d2e56fbdeb4251b1e7f14e0e38ae5fc99f5bfaf386b6958d51be275	add=2,del=3;	internal/license/license_test.go
tracked	 M	6b7a49552c279c686b443a77480179694eb7b03c541091edad177f1eef5fe446	df100dd7021fe02b6b3c68f96aa83b6c1333354fa010798113a95dd9c0b9c56e	add=32,del=25;	internal/prompts/integration.md
tracked	 M	460f00d256f4b2610ba4f459bc239fe38ec993e7419eb42955662514c84599e7	b67a760c6d2b28e2ebaecb2fa78702139b04a75e35605121b00c049d21ede020	add=2,del=1;	internal/prompts/prompts.go
tracked	 M	4d02822b146e9c8c12542110a8e06ef02d40161b48c087275b46e1120c6d80df	1c49720b84263571df792a6cfc05ecf323279074079666fef13c37269086fda9	add=3,del=3;	internal/prompts/prompts_test.go
tracked	 M	2982ed38205e550823fc2e6c782e795283f4d808bba5a6f0d145aa71534a074d	96b37bb6a274ab1fa16469258c4449523e91c893b2eb6969cabc42c1f0123569	add=1,del=1;	internal/quarantine/allowlist.go
tracked	 M	5ac6ae6740a2b354e04cea1331c984068fe5692c9cbdaf5523512be6d743910e	c5efe64af586782591924633a84009ba3a2ff679165617e73aa53503e30a62d8	add=9,del=7;	internal/quarantine/policy.go
tracked	 M	93779810be6d95c6ad717c0b5c3d33155dbd1087876f15fee5eb8b1171a5732b	9fe8f26a2b93abed6c3b5691bd3fd952c1a9d9caa946af4bf8f514789ccdfdd3	add=8,del=8;	internal/quarantine/quarantine_test.go
tracked	 M	5e4d461ab73a791103cb801004c3eac85e3164336e5e350c94a02633648ca733	a7a19cad37f47c3d894af6bc8eefa1ca0eb93d5e3cad6ece9894202ddb78c9b4	add=1,del=1;	internal/quarantine/resolvers/resolver.go
tracked	 M	4442dcf4e84b20b04966ff46a0b7b1b296715fecc3dd436bffbd671dc87e07f6	5b099f3a1edaf08dcd5a01a5125df01c46b8d8ed64db05283691828b80e37a83	add=3,del=2;	internal/server/server.go
tracked	 M	fc41d9309a1f984a1329a12804e61a7fbeb19fa9ddd3e8e4943c33939d50ac64	b0fe1e04063b52932c0815810aea9939778741ef8678b9d9a9804b180973ca79	add=3,del=3;	internal/tray/tray.go
tracked	 M	152773abea1a2f92776adeb8f2cd75cc15a2b4e1aff07e32314804665b194420	d85b09eaf70b21bd9361e8075087a998bd33b32cfb272c51fc9d70f310b3524f	add=5,del=8;	tests/integration/license_trial_test.go
tracked	 M	1e951dc09e301f9f85f4e4e0fc5a5b18b77c1727bc3291f47867f7a62e6252fc	c314a8c8c629da5fb92c82b06e7b01ac783fad57ec618193fc1b11d134c848c1	add=0,del=2;	web/src/admin/pages/Settings.tsx
tracked	 M	1a016c811d7b5ce0ba097218e03551112b5efba790c848ba65d17cb3ab9330c8	fbaa34601a65f1958297e58efa3c764a719e4d674c44e8dc23f9bdd91579a338	add=14,del=24;	web/src/i18n/en.ts
tracked	 M	c68f16873d365b71a56c708c275bcd7f27f3e36a86445eb47c903e6bc21365f3	6f51eefbdee9d274fcfb0f0444bb854737fe1ea196355fa398b69defdf914357	add=14,del=24;	web/src/i18n/zh.ts
tracked	 M	4a8d19ebd0ecd021cda7d0d983aa3ae2607bd12c1be1f449115368557ad862f0	f4d75a0021cdfe6b3a4c73e7a55f2518e41304ea6cd7dc1f4e4f88c5a30cdaf0	add=2,del=2;	web/src/lib/api.ts
tracked	 M	829d37340ca3b927b36ddbcdb2e659a09c2fbe2f05abe59124535d7a4ede23e5	235ec4d00664959bec1d9d9af0fc31dff8be801b38d702542bcf663c9faf0411	add=17,del=51;	web/src/lib/ecosystemData.ts
tracked	 D	DELETED	0d4a08a118d26e5ec7e111df7a7bc85f62bae7c15b7e68034fabf23e079ed2ab	add=0,del=84;	web/src/portal/components/AllInOnePane.tsx
tracked	 M	214f5dd17831dac1295dbdb16077936a61a70bdc189453767c9c8c6633a598b4	1ed77fd0300c7544a8203810f12a861dbd8f3a11f6dc5da73b35fe5d9ede14ab	add=11,del=1;	web/src/portal/components/ConfigurePane.tsx
tracked	 M	1bd98d8284fd86189910f5e6c5f53d512119b87fe0e760b93c1bd03adb8154a9	9bb847b7333b2910ccef4af5c254fb043e49884e9a9b2e7bfae7fbea8b105c80	add=2,del=4;	web/src/portal/components/IntegrationPromptButton.tsx
tracked	 D	DELETED	d5ab13dd7a0f206e94ca8ec95e41c7ac87b427384113b7bfd3ce4b3cc6b8941a	add=0,del=150;	web/src/portal/components/LanguageRail.tsx
<!-- DIRTY_EVIDENCE_END -->
```

## Starting-Dirty Overlap Policies

Fields are tab-separated: path, ordered owners, initial disposition, semantic guard ID.

```text
<!-- OVERLAP_PATHS_BEGIN -->
internal/config/config.go	Plan03-T1	mixed	config-existing-semantics
internal/api/router.go	Plan01-T6>Plan02-T5>Plan03-T7	mixed	router-existing-semantics
internal/server/server.go	Plan02-T3>Plan02-T5>Plan03-T7	mixed	server-tamper-semantics
web/src/admin/pages/Settings.tsx	Plan02-T6	adopted	settings-no-postgres
web/src/lib/api.ts	Plan01-T8>Plan02-T6>Plan03-T8	mixed	api-open-audit
web/src/i18n/en.ts	Plan04-T6>Plan02-T6>Plan04-T7>Plan04-T9>Plan04-T11-if-edited	preserved	i18n-current-product-copy
web/src/i18n/zh.ts	Plan04-T6>Plan02-T6>Plan04-T7>Plan04-T9>Plan04-T11-if-edited	preserved	i18n-current-product-copy
config.example.toml	Plan02-T1>Plan03-T9>Master-T8	preserved	config-example-supply-chain
README.md	Plan03-T9>Master-T8	preserved	readme-supply-chain
docs/README_zh.md	Plan03-T9>Master-T8	preserved	zh-readme-supply-chain
DESIGN.md	Plan04-T12>Master-T8	mixed	design-instrument
CHANGELOG.md	Master-T8	preserved	changelog-supply-chain
docs/self-test-checklist.md	Master-T8	mixed	self-test-operator-truth
<!-- OVERLAP_PATHS_END -->
```

## Pending Overlap Evidence

Before committing, each owner appends one row: path, exact subject, disposition, staged diff SHA-256, semantic guard ID, pending-review state, and UTC record time.

```text
<!-- OVERLAP_PENDING_BEGIN -->
internal/api/router.go	fix(auth): enforce admin route capabilities	preserved	945ca5fe5abefe1289aa8a427afd70266de06b9e5de4a441d7855d5b8da2ddfa	router-existing-semantics	pending-review	2026-07-11T09:05:17Z
<!-- OVERLAP_PENDING_END -->
```

## Approved Overlap Reviews

After both reviewer turns approve, the controller appends one row: path, exact source subject, approval bookkeeping subject, disposition, source commit SHA, staged diff SHA-256, guard ID, spec review reference, quality review reference, and UTC approval time.

```text
<!-- OVERLAP_APPROVALS_BEGIN -->
internal/api/router.go	fix(auth): enforce admin route capabilities	chore(plan): approve Plan 01 Task 6 overlaps	preserved	07caebaa9d5b4f586bb68aff5d72722156f69cb3	945ca5fe5abefe1289aa8a427afd70266de06b9e5de4a441d7855d5b8da2ddfa	router-existing-semantics	spec-approved:Plan01-T6	quality-approved:Plan01-T6	2026-07-11T09:10:19Z
<!-- OVERLAP_APPROVALS_END -->
```
