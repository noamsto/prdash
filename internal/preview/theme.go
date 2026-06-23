package preview

import (
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
)

// darkStyle is glamour's built-in dark chroma style. We deliberately do NOT
// post-process rendered output (no pipe-stripping), so tables render intact.
var darkStyle ansi.StyleConfig = styles.DarkStyleConfig
