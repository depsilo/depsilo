package config

// CurrentConfigVersion is the newest on-disk configuration schema understood
// by this binary. Missing versions are accepted as the legacy schema zero;
// newer versions fail closed so an older binary cannot silently discard keys.
const CurrentConfigVersion = 1
