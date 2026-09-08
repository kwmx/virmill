# File-browser default correction

Source `4489c6e7b267acd18708237eeeaa90900f74b255` removes the test-specific
~/images preference. Empty fields open the user's home, even when images exists;
existing field paths retain their own location. No new managed-storage directory
is introduced. Product paths continue to follow the documented XDG contract.

- `picker-home-tests-001`: focused picker/import-opening race checks passed.
- `picker-home-artifacts-001`: three package/private-IPC/installer checks passed.
- `picker-home-native-001`: installed CLI passed at 80×24 and 120×36. The
  explorer opened at home despite the existing images folder; the fixture then
  explicitly navigated into that folder, selected a source and canceled. Guests,
  jobs, binary and all six media entries' metadata were preserved. No import or
  guest operation was submitted; plan previews only.
- `picker-home-upgrade-001`: core RPM replacement passed, preserving coordinator,
  guests and jobs. Helper and backend behavior unchanged.

Installed CLI SHA-256:
`0625f42d0e27ba3c19b86b2bfb8107495ff9a79855890190c3ee4460e727734e`.
Core RPM SHA-256:
`4fe5502990a7accf2a124229c8202b71aeae3082c8a8165ff34fb538833f502b`.
All built package hashes are in dist/checksums.json and the retained local build/picker-home-packages.json output.
Remote evidence: ~/virmill-tests/picker-home-4489c6e.
No dependency changes or acceptance-status promotion; all 71 scenarios remain required.
