# Changelog

## [0.2.0](https://github.com/jessechumo/Wera/compare/wera-v0.1.0...wera-v0.2.0) (2026-10-11)


### Features

* **api:** add a community blog with comments and reactions ([93cb090](https://github.com/jessechumo/Wera/commit/93cb090c551b199299e710b46fbfcd18293b8283))
* **api:** add interview practice questions ([091cd58](https://github.com/jessechumo/Wera/commit/091cd58728512c0c684af1eacd608dc96dcea63d))
* **api:** add profile pictures and resume file viewing ([9193427](https://github.com/jessechumo/Wera/commit/9193427465feff3b78068a9deb5a4a0022f3723c))
* **api:** add resume documents, previews, PDF and tailoring per job ([85d9614](https://github.com/jessechumo/Wera/commit/85d9614cb31ea9457033d66ef04cfefb58b52981))
* **api:** add the browser extension endpoints ([47e5619](https://github.com/jessechumo/Wera/commit/47e56198aec10a77b9a999958ea4f7d85eebf0a2))
* **api:** add user settings, hidden companies and account deletion ([ac863e0](https://github.com/jessechumo/Wera/commit/ac863e0595ba6b97eb079c55d844d0d149c205b0))
* **api:** filter and rank synchronously when a profile is saved ([130a6fc](https://github.com/jessechumo/Wera/commit/130a6fc24628722cef1dcf2ee2f8df6ad01d80aa))
* **api:** generate cover letters for a job ([88ad4f8](https://github.com/jessechumo/Wera/commit/88ad4f89eb4e16e7baeae865053fdc5a44a58266))
* **api:** list role families A to Z ([bade9bd](https://github.com/jessechumo/Wera/commit/bade9bda5046acde27ccce7618ee9b4e8acea9fb))
* **api:** show community authors' profile pictures ([ca0f1b1](https://github.com/jessechumo/Wera/commit/ca0f1b154aebfd0025593890e1d73bc379b06de3))
* **api:** show estimated matches instantly and score jobs on demand ([d4528df](https://github.com/jessechumo/Wera/commit/d4528df264eb8d6d738e092821a999843e1dfe69))
* **api:** summarize visa sponsorship signals by company ([63d439b](https://github.com/jessechumo/Wera/commit/63d439b84ec5ef1d5c71e0f182038f8b124166cb))
* **build:** stamp binaries with their version and commit ([f545f6d](https://github.com/jessechumo/Wera/commit/f545f6ddc7b97e79e63938d6bba71fc8d8c5039b))
* **config:** make the sign-up rate limit configurable ([e673eb6](https://github.com/jessechumo/Wera/commit/e673eb6cfffa2123265cf361175439cfb1159423))
* **moderation:** review community content before it is published ([1114972](https://github.com/jessechumo/Wera/commit/1114972e2757dc34d5fd9ed85dfe043f8d182148))
* **pipeline:** apply known job facts when filtering ([97e6dec](https://github.com/jessechumo/Wera/commit/97e6decacc1def0496b4d2df2c61948c9563f07d))
* **pipeline:** defer companies during maintenance and catch up after ([dc24aac](https://github.com/jessechumo/Wera/commit/dc24aacbf4702d9187efbdab8b5be445c536fdbe))
* **pipeline:** rank unscored matches locally before LLM scoring ([5011f5f](https://github.com/jessechumo/Wera/commit/5011f5f8448ccb5073bb796179bd19c421ccdf55))
* **profile:** read job pages, draft answers and tailor resumes ([9ee213b](https://github.com/jessechumo/Wera/commit/9ee213b7b672f704d6f3ef58a1f919dc8cc6f182))
* **resume:** import resumes with AI and tailor them to a job ([25cba19](https://github.com/jessechumo/Wera/commit/25cba19b341989fb2c53b3c39814f8880c8f0b19))
* **resume:** structured resumes rendered in the classic LaTeX layout ([f9dfaf0](https://github.com/jessechumo/Wera/commit/f9dfaf0997a292af30b228ade1805e8174d731ae))
* **store:** add extension tokens, application details and added jobs ([575a9fe](https://github.com/jessechumo/Wera/commit/575a9fe71eeac3b289f9ba09b79a9f8594fa74a5))
* **store:** add extension tokens, application details and added jobs ([3a8b660](https://github.com/jessechumo/Wera/commit/3a8b6600913bc42ee1b8ef6c4bf948f36c5ef039))
* **workday:** recognize job board maintenance ([795057e](https://github.com/jessechumo/Wera/commit/795057ec1946eec560651d0ee7cd39936826d3d1))
* **workday:** recognize job board maintenance ([44236a3](https://github.com/jessechumo/Wera/commit/44236a30fa4b5ba25e588f018ec88f7694d47bd0))


### Bug Fixes

* **api:** cap application bodies and add server timeouts ([c167658](https://github.com/jessechumo/Wera/commit/c1676585305dd57a349d8124ddac64998b53137f))
* **api:** count new matches per day in the user's time zone ([0182b99](https://github.com/jessechumo/Wera/commit/0182b993540fd64abf8b36388fbdd93ca3f6e75e))
* **api:** count new matches per day in the user's time zone ([6580b52](https://github.com/jessechumo/Wera/commit/6580b52f4c9bc1933bac6d8906a742b1bea71cfd))
* **api:** find a saved job from its /apply page ([7810ed4](https://github.com/jessechumo/Wera/commit/7810ed49554fd52c3abe3e5a44701af2b416b15b))
* **auth:** limit logins per account, stop IP spoofing, add headers ([b2d2f0a](https://github.com/jessechumo/Wera/commit/b2d2f0aa0946d247df6ff417f8fd5d6c056da412))
* **config:** require HTTPS for the inference API ([7639ed7](https://github.com/jessechumo/Wera/commit/7639ed7458d2249a0510f27051598bf8e56bb5ca))
* **deps:** upgrade pgx, x/image and x/text for security advisories ([4004a2e](https://github.com/jessechumo/Wera/commit/4004a2e2ede502695278100f4e8c91424defc26b))
* **llm:** fence untrusted text in every prompt ([5e831ed](https://github.com/jessechumo/Wera/commit/5e831ed0c15d94efa799ec5eed1a2d02bdbd0b5b))
* **pipeline:** restore existing scores when jobs are refiltered ([b7cdf95](https://github.com/jessechumo/Wera/commit/b7cdf954986c84913f558913407c2f316d4b4ea8))
* **relevance:** scale estimates by similarity so top matches differ ([4318126](https://github.com/jessechumo/Wera/commit/4318126c0e113b33bb8efdda262c068d1bcd857c))
* **resume:** count a keyword met when any named alternative appears ([37ac697](https://github.com/jessechumo/Wera/commit/37ac697407f3934e7d655e84efa1d51222890c1b))
* **resume:** hide bullets in a tailored copy only when the page is full ([ae4f280](https://github.com/jessechumo/Wera/commit/ae4f28037e32fab68481b8550b47ae738ddd6bf4))
* **sources:** retry when a response breaks off mid-body ([a3dceca](https://github.com/jessechumo/Wera/commit/a3dcecaf1659f53c3550c62a3c97345c21f26f1e))
* **sources:** stop retrying when a board asks to wait over a minute ([5c17876](https://github.com/jessechumo/Wera/commit/5c178764115145ca47abda99245ee9c29dabda13))


### Performance

* **fetch:** send ETags so unchanged boards answer 304 ([38b0d33](https://github.com/jessechumo/Wera/commit/38b0d33d172f4be87c86a08cc0f8f4b401d0c942))
* **filter:** filter a new user's 40k jobs in seconds ([303e34c](https://github.com/jessechumo/Wera/commit/303e34c0d0f3c357d915b1b3fa2e9c8bffbd1098))
* **scoring:** extract job facts once and share them across users ([e90fdf2](https://github.com/jessechumo/Wera/commit/e90fdf2be09c8f69d4a0c127e7108c60fe2ab723))
* **scoring:** score in priority order and save each result as it lands ([63f3c04](https://github.com/jessechumo/Wera/commit/63f3c04af9eb7f96c8f76f5adc91cf52a1982a97))
* **store:** write only new or changed postings ([c7e01fa](https://github.com/jessechumo/Wera/commit/c7e01fa439dd44db699222fd2b3fed49f734409d))
* **workday:** list only new postings, sweep the full board daily ([11c8bfd](https://github.com/jessechumo/Wera/commit/11c8bfd92585a643a60bbee988890fc0b873aff4))


### Refactoring

* wrap errors with %w and compare them with errors.Is ([6c8f641](https://github.com/jessechumo/Wera/commit/6c8f64136d3045497c21d1c1e4c379cf84ed41cc))


### Documentation

* add a security policy and summary of protections ([886f8d1](https://github.com/jessechumo/Wera/commit/886f8d173ea47bd86adc4c395e61f8a19317f7fb))
* add architecture and pipeline diagrams, refresh the README ([a7c9cf1](https://github.com/jessechumo/Wera/commit/a7c9cf1ed97ec765a9bb50d287dd288b54b4a383))
* describe resumes, tailoring and the typst requirement ([e5e1d88](https://github.com/jessechumo/Wera/commit/e5e1d88780ac1322d00a1f671442f741024693b8))
* describe the Chrome extension API in the README ([b8db0ba](https://github.com/jessechumo/Wera/commit/b8db0ba9b697f1d48a4c3bfb0ad03421baf43d2a))
* README — add wera deep to quick start ([fb5e651](https://github.com/jessechumo/Wera/commit/fb5e65134e50000f8bf723a24d4564ca9ce83cb1))
