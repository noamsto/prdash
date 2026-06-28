package preview

import (
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
)

// darkStyle is glamour's built-in dark chroma style. We deliberately do NOT
// post-process rendered output (no pipe-stripping), so tables render intact.
var darkStyle ansi.StyleConfig = styles.DarkStyleConfig
