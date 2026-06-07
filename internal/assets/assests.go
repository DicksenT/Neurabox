package assets

import _ "embed"

//go:embed rtk-windows.exe
var RTKBinary []byte

//go:embed neuragraph.exe
var Neuragraph []byte