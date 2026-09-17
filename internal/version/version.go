package version

// Version is the build version reported in the JSON report. The Makefile
// overrides it with -ldflags; an unset build reports "dev".
var Version = "dev"
