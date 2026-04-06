/*
** sqlite3.h — compatibility shim for sqlite-vec + mattn/go-sqlite3.
**
** mattn/go-sqlite3 ships sqlite3-binding.h (the SQLite public API) instead
** of the standard sqlite3.h to avoid linker symbol conflicts. sqlite-vec
** expects sqlite3.h, so this shim bridges the two.
**
** CGO_CFLAGS must include the mattn/go-sqlite3 module directory so that
** sqlite3-binding.h can be found. See the package documentation.
*/
#pragma once
#include "sqlite3-binding.h"
