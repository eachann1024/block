package main

// Enable MyGo's Unix-socket native UI automation plugin whenever MYGO_AUTOMATION
// is set in the environment (zero overhead and inactive when MYGO_AUTOMATION is unset).
import _ "github.com/egoist/mygo/plugins/automation"
