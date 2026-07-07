package preview

import (
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
)

// darkStyle/lightStyle are glamour's built-in chroma styles. We deliberately do
// NOT post-process rendered output (no pipe-stripping), so tables render intact.
var (
	darkStyle  ansi.StyleConfig = styles.DarkStyleConfig
	lightStyle ansi.StyleConfig = styles.LightStyleConfig
)

// activeStyle is what Render builds renderers from; SetMode swaps it.
var activeStyle = darkStyle
