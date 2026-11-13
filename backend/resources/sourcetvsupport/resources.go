package sourcetvsupport

import _ "embed"

const Version = "a19d90c-sm1.12"

//go:embed linux-sm1.12.zip
var Linux []byte

//go:embed windows-sm1.12.zip
var Windows []byte
