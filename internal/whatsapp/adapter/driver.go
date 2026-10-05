// This file blank-imports the SQLite driver for its side effect.
//
// It is separate, and commented, because a blank import inside a file with real code
// reads as an accident and later gets "cleaned up" by someone who does not know what
// it is for. The import is what registers the driver name "sqlite" with database/sql;
// without it, sql.Open("sqlite", ...) fails at run time with "unknown driver", long
// after the code compiles cleanly.
//
// This is the pure-Go SQLite translation, and that is not a free choice: it is what
// keeps CGO_ENABLED=0 working, and therefore what keeps the shipped binary static.
package adapter

import (
	// Registers the "sqlite" driver name with database/sql. See above.
	_ "modernc.org/sqlite"
)
