
## [0.5.2] - 2026-07-07

### Features

- Support extracting and opening gitlab pipelines via gitlab-tui ([01dc40b](https://github.com/nospor/outlook-tui/commit/01dc40b74b487169b801e7f09c952f25c386ca56))

### Miscellaneous Tasks

- Update CHANGELOG.md for v0.5.1 [skip ci] ([5a951c1](https://github.com/nospor/outlook-tui/commit/5a951c18ef63a834725a09c1153397d27e156289))

## [0.5.1] - 2026-07-06

### Documentation

- Add repository links and descriptions for yt-tui and gitlab-tui ([92e2797](https://github.com/nospor/outlook-tui/commit/92e2797bb8a09b0cf70c0416d07f39211f39922d))

### Miscellaneous Tasks

- Update CHANGELOG.md for v0.5.0 [skip ci] ([e43f056](https://github.com/nospor/outlook-tui/commit/e43f056cc44ce2a45ee36b383ff29b99e14b358e))

## [0.5.0] - 2026-07-06

### Features

- Add desktop system notifications for new unread inbox messages ([d5df9d8](https://github.com/nospor/outlook-tui/commit/d5df9d86be2e3a5a6e500f9a3cf358b2118199c8))
- Prioritize Inbox and Sent Items at the top of the folder list ([b220d76](https://github.com/nospor/outlook-tui/commit/b220d76418a6067e2cb6bf2155d2b9431f91bcb3))
- Support html entities and text styles in email body rendering ([8966d01](https://github.com/nospor/outlook-tui/commit/8966d01de7b4b86a13eb1a17a478b3588cc3638d))
- Make background refresh interval configurable ([f7b08f3](https://github.com/nospor/outlook-tui/commit/f7b08f375b043de512607f043d2d9267fe569b6e))
- Open attachments with xdg-open after saving ([5c035c4](https://github.com/nospor/outlook-tui/commit/5c035c44915b666712f6ed366acd533f4e09788a))
- Add reply email functionality via 'A' keybinding ([f031317](https://github.com/nospor/outlook-tui/commit/f031317267d9afd3274c3312b4a511d15c7d329a))
- Display To and Cc recipients in message detail view ([f63dc86](https://github.com/nospor/outlook-tui/commit/f63dc862217af272755b8ea19b48b40a6c7048c1))
- Support Reply All and multiple email recipients ([6695045](https://github.com/nospor/outlook-tui/commit/6695045f65647f1b9b9a789b34fc5d1db8ce3726))
- Add layout 2 (stacked folders/messages + wider detail pane) ([fc5b2de](https://github.com/nospor/outlook-tui/commit/fc5b2de884b41b6f7b523721e0d3fae8f0e68eda))
- Group messages into collapsible conversation threads ([f2418e4](https://github.com/nospor/outlook-tui/commit/f2418e45ff686f93d0ae8d272cdb5ad824423a2d))
- Add optional SQLite cache for offline message loading and instant detail preview ([4829271](https://github.com/nospor/outlook-tui/commit/4829271b713a1d704cc04cb01b5b4dc6cea194fd))
- *(tui)* Auto-detect and dim original/quoted messages in detail view ([e7e021e](https://github.com/nospor/outlook-tui/commit/e7e021ed8c1bf887864659d43fd3366774793b5c))
- *(tui)* Highlight URLs in message details ([ce44fbc](https://github.com/nospor/outlook-tui/commit/ce44fbccc15939245f8b1206a02f62a122831b41))
- Add contacts autocomplete popup from SQLite database when composing mail ([f8aad1e](https://github.com/nospor/outlook-tui/commit/f8aad1ef37898f2d17e15671b3cfc09bd2c64ad1))
- Add CC field support in mail composition and reply all ([82c5114](https://github.com/nospor/outlook-tui/commit/82c5114d87fe284e7a1827d1509aa523dbbd2a87))
- Add 'u' keybinding to copy URLs from main message to clipboard ([bd03b8d](https://github.com/nospor/outlook-tui/commit/bd03b8dc82137d49cb418f660b8c63c981e16b1d))
- Open compose body in external editor via Ctrl+g ([08617e7](https://github.com/nospor/outlook-tui/commit/08617e7330c6fb10a890bff9141e0ef41fcdb652))
- Add 'R' key binding to reload the selected folder ([5316a2d](https://github.com/nospor/outlook-tui/commit/5316a2d502a4cda3c971f65a70e7904807c72462))
- *(tui)* Add message recovery shortcut and split footer into two lines ([8222e83](https://github.com/nospor/outlook-tui/commit/8222e83980c16ca8e678f7c746334bb7a15acbc6))
- Add message pagination & fix fast scrolling layout duplication ([50c3391](https://github.com/nospor/outlook-tui/commit/50c33919c08f29570f1accf5adad7196670f52f1))
- *(tui)* Show date and time next to author in Layout 2 messages pane ([9707171](https://github.com/nospor/outlook-tui/commit/9707171430bfbe45887312fbb531fcd7f8e303c4))
- Bypass reply confirmation if only one external participant is in thread ([cc4bceb](https://github.com/nospor/outlook-tui/commit/cc4bcebbe66c42b78f39f8b732ff3f3816e8b08f))
- Add YouTrack integration via yt-tui on keypress 'o' ([382d877](https://github.com/nospor/outlook-tui/commit/382d877b647535cad640382908b7ce9fd191cc11))
- Add image preview support using Kitty graphics protocol in attachments view ([6d2ca78](https://github.com/nospor/outlook-tui/commit/6d2ca78a6e5affbb4643bf78dcba0295b9755a47))
- Add `excluded_folders` option to config ([90a8db2](https://github.com/nospor/outlook-tui/commit/90a8db234aba8162c6c0a8c9e22580bef7f0552d))
- Add local Favorites folder with keybind 'f' toggle ([7272149](https://github.com/nospor/outlook-tui/commit/727214994d344fe41a73da2b7568b9e3e60c857f))
- Make message detail pane scroll amount configurable ([825bdab](https://github.com/nospor/outlook-tui/commit/825bdab7be42653c770ea0bc8f8f3d0f66995f07))
- Add scrollable help popup with keyboard shortcuts guide ([9d8ab07](https://github.com/nospor/outlook-tui/commit/9d8ab0726b37983c37ab6f43effca42f4c119fb7))
- Add Ctrl+V clipboard image paste support in compose mode ([6481ec7](https://github.com/nospor/outlook-tui/commit/6481ec79c1da00fbc728dd84f2f92710040f0ce6))
- Add file attachments support to mail compose and replies ([f2b1721](https://github.com/nospor/outlook-tui/commit/f2b1721b105062eb77f3f68371380d0ccc80e84c))
- Fetch and preview inline image attachments in attachments pane ([8f5530b](https://github.com/nospor/outlook-tui/commit/8f5530bd3a1b957271374e0682e946f9ff9bbd38))
- Add confirmation prompt when discarding compose draft with filled body ([2c05b96](https://github.com/nospor/outlook-tui/commit/2c05b961f43f239fe1ace8fb0f4ba0eec100abda))
- Add image_viewer configuration option for image attachments ([b5362e0](https://github.com/nospor/outlook-tui/commit/b5362e0e621dce19d2f3da61d198b0e73e95df81))
- *(tui)* Position cursor after reply before quoted block on editor exit ([dcdaf29](https://github.com/nospor/outlook-tui/commit/dcdaf29a72cd88b70462c02c13d532d03252ccea))
- Add HTML rendering support for lists, headings, and tables ([2f943b6](https://github.com/nospor/outlook-tui/commit/2f943b6dafc266ce6025096454cb224ef7a10f87))
- Add capital D shortcut to delete entire message thread with confirmation ([dc02182](https://github.com/nospor/outlook-tui/commit/dc021823ed8c4ab818a09ed596f31fac7a9e8ebe))
- Replace 'u' shortcut with 'y' yank combination modal ([6a252aa](https://github.com/nospor/outlook-tui/commit/6a252aae91a4880114b8c8dffa475feeecdf39f5))
- Support downloading remote inline images as attachments ([63001a7](https://github.com/nospor/outlook-tui/commit/63001a75e787276444d30ccdbe30bf3931bff752))
- *(tui)* Load all image attachments in external viewer starting at selection ([71779c3](https://github.com/nospor/outlook-tui/commit/71779c303acf1de822018ab9417e0a7c55601819))
- *(tui)* Prefix attachment downloads with message ID hash to prevent duplicates ([43601c9](https://github.com/nospor/outlook-tui/commit/43601c9b20b2835ae307db56f1e247296c23adab))
- Make attachment download directory configurable ([bb5b132](https://github.com/nospor/outlook-tui/commit/bb5b13247e937b5f091073536bb13c1c6ecf34fd))
- *(tui)* Overlay delete thread confirmation as a modal popup ([ddc9486](https://github.com/nospor/outlook-tui/commit/ddc948606684c08b642d3637b56a1c1e825fdeb9))
- *(tui)* Convert attachments screen into an overlaid modal popup ([08037ea](https://github.com/nospor/outlook-tui/commit/08037eaaaa57e11fd917d9dbc5386e5f7f625724))
- Add code syntax highlighting, diff formatting, and clean layout rendering ([cd8c6b6](https://github.com/nospor/outlook-tui/commit/cd8c6b605d3f2699f85bbb915095dd5a105a6f6d))
- Integrate gitlab-tui and support GitLab merge request links ([a607cbc](https://github.com/nospor/outlook-tui/commit/a607cbcdcdeb3782275b69cc176c803fb03cde70))
- Sound terminal bell when new email notification triggers ([fccc8eb](https://github.com/nospor/outlook-tui/commit/fccc8eb3e598056ff472a572bb96a3af4ca8b21f))
- *(tui)* Make message auto-read behavior focus-aware ([aeb3fd0](https://github.com/nospor/outlook-tui/commit/aeb3fd085a44a72df65432c27b25029f6bf8aa88))

### Bug Fixes

- Making app working ([8921dee](https://github.com/nospor/outlook-tui/commit/8921deed51975091bcdbd64765a01c6d6628c90e))
- Preserve selection index on message deletion and reset on folder switch ([bdd6b26](https://github.com/nospor/outlook-tui/commit/bdd6b26df73aad6b73ff46261cb218b8d18b9b42))
- *(tui)* Wrap long email body lines in detail viewport ([b7bc177](https://github.com/nospor/outlook-tui/commit/b7bc177f11bd7a3be6a90e9ad6ad7d1cf5f763b6))
- *(tui)* Shorten detail pane in layout 2 to align with left panes and restore footer ([cec71b6](https://github.com/nospor/outlook-tui/commit/cec71b6cef45321e52e389b128957225a685dac6))
- Load first message details on startup and folder switch ([41999bc](https://github.com/nospor/outlook-tui/commit/41999bcf193ff804829bdc94a3462532cfff7c86))
- Prune deleted messages from SQLite database cache on sync ([651997c](https://github.com/nospor/outlook-tui/commit/651997c6fd86c5656c78929460665065b301ebe5))
- Separator ([50d5f29](https://github.com/nospor/outlook-tui/commit/50d5f297d38dab3f3ce6e62667e539b588245375))
- *(tui)* Prevent vertical layout jumping and missing footers in multi-pane views ([cb0e33a](https://github.com/nospor/outlook-tui/commit/cb0e33ac7e5f7aa5eab19e795121c780c0404ca2))
- *(tui)* Resolve compose send keybinding issues and empty Cc field serialization error ([d72de64](https://github.com/nospor/outlook-tui/commit/d72de64de24c2dd7ae619b8c946e23474224d26f))
- Thread replies correctly using Graph API reply endpoints ([8c10644](https://github.com/nospor/outlook-tui/commit/8c106440f8b6c447590583b97e76bc0a179a988d))
- Prevent adjacent text from being absorbed into anchor-tag URLs ([02fb81d](https://github.com/nospor/outlook-tui/commit/02fb81d6d5e01d2b2f85f5fb15e7d2f7d4195fae))
- *(compose)* Fix body textarea indentation and remove line numbers ([90f8fe4](https://github.com/nospor/outlook-tui/commit/90f8fe4783bf5b2c612fb69b44192abb682b9f55))
- Db problems ([de055e9](https://github.com/nospor/outlook-tui/commit/de055e9b27991b94ed61d167ffe84130ffd2da5f))
- Retrieve and display attachments for cached messages on startup ([590eee7](https://github.com/nospor/outlook-tui/commit/590eee76be3945ee0f00b5d79327a3656deef26a))
- *(tui)* Strip newlines and collapse whitespace in grouped message previews ([b925a2c](https://github.com/nospor/outlook-tui/commit/b925a2c00c5da97931ecf6b704e124b7475b7862))
- Update folder unread counts dynamically when messages are read, deleted, or toggled ([066d355](https://github.com/nospor/outlook-tui/commit/066d35511d05a4a187b1b9b4f08ad940029391ba))
- *(tui)* Keep cursor focus on thread header when collapsing thread ([f210ff2](https://github.com/nospor/outlook-tui/commit/f210ff2c65ba2eb11cba304f5fd4f66826190688))
- *(tui)* Prevent message truncation on raw '<' characters ([1116d87](https://github.com/nospor/outlook-tui/commit/1116d87b3367f17c0b4a370db52bec4b48fb6ac7))
- Retain attachments view after opening/saving an attachment ([10cbb24](https://github.com/nospor/outlook-tui/commit/10cbb24509148944a9b76bf2fcb8dec746837304))
- *(graph)* Preserve newlines in reply and reply-all message bodies ([d8afbca](https://github.com/nospor/outlook-tui/commit/d8afbca627884ee16c82e4303afa64ebefbfc62b))
- *(tui)* Position compose textarea cursor at the very top when replying ([fbad3f6](https://github.com/nospor/outlook-tui/commit/fbad3f62487f03c3b1a6e36bb636863a8ffc5216))
- Restore link styling after nested ANSI reset codes ([31e4da8](https://github.com/nospor/outlook-tui/commit/31e4da80163ab103180fc6675deb1bef40db3b3f))
- *(tui)* Strip zero-width characters and normalize Unicode spaces in body formatting ([cba2cca](https://github.com/nospor/outlook-tui/commit/cba2cca5a2cc0494ac89d1fcbe6efba28e0af3bb))
- *(tui)* Load configuration at startup to prevent empty Client ID prompt on token validation failure ([87461e7](https://github.com/nospor/outlook-tui/commit/87461e786f704e04266c38f36a9955ee349d0067))

### Other

- Add capital J/K cross-pane navigation and spacebar thread toggling from folders pane ([232bb68](https://github.com/nospor/outlook-tui/commit/232bb68fd91a38adc2fd27a8dc71cc092cdce2dc))
- Initialize compose body height dynamically from the start ([fcde024](https://github.com/nospor/outlook-tui/commit/fcde0247ca3b2fc52c47b37f3ffd838bf066e904))

### Refactor

- Remove terminal image previews for attachments ([5e7c24e](https://github.com/nospor/outlook-tui/commit/5e7c24e5a28175169dd9254f5e00895350397ced))
- Swap 'r' and 'R' key bindings ([22257ed](https://github.com/nospor/outlook-tui/commit/22257ed7084c427085378b8808bdca66c0ff372b))
- *(tui)* Move URL yank selection to a modal popup overlay ([f27ca99](https://github.com/nospor/outlook-tui/commit/f27ca9910339369a25d5e91a68f022c99fa283fc))

### Styling

- Modernize theme and layout colors in TUI ([944c985](https://github.com/nospor/outlook-tui/commit/944c9851a38c295407df43e93af10d8ec99614b9))
- Adapt UI theme to Catppuccin Mocha ([0c5a0e6](https://github.com/nospor/outlook-tui/commit/0c5a0e613a2db3349a3f674dac22bfac7504d1c3))
- Update key bindings in help text ([d2bc6b1](https://github.com/nospor/outlook-tui/commit/d2bc6b1cdb6bf92c9ff8406e77339f7b414f546d))
- *(tui)* Move pane labels to top border bar ([6d391c3](https://github.com/nospor/outlook-tui/commit/6d391c3cfe8b14ca38a76e19d5e63c5665bc2680))
- Hide HTML URLs in message details and style link text in blue ([d84117c](https://github.com/nospor/outlook-tui/commit/d84117cb318f05dc4b3871d8549911270cb5de88))
- *(tui)* Widen help popup columns to prevent text wrapping ([ab1fa34](https://github.com/nospor/outlook-tui/commit/ab1fa3454d8485d5ec4432c35ea9bc3b79c4362a))

### Miscellaneous Tasks

- Add git-cliff configuration and GitHub workflow for automated changelog ([933506b](https://github.com/nospor/outlook-tui/commit/933506bf504dfac490003b08e24e8424932dc92d))
