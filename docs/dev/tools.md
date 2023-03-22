# Tools

We use a number of tools for things like code generation, linting, and developer convenience. These tools have their versions controlled by the module in `internal/tools`.

## Install and Use

To install all tools, you can run the `install-tools` make target:
```
make install-tools
```
This target changes into the `internal/tools` directory, and uses the versions in that `go.mod` file for all installations.  
Each tool has a corresponding make target for usage, or you can use the `precommit` and `presubmit` targets to run a number of checks in order.

## Update a Tool

To update a tool, run `go get` in the `internal/tools` directory:
```
go get <module name>@<desired version>
```
Or you can simply get the most up to date version:
```
go get -u <module name, or leave empty to update all tools>
```
Once you have updated the tool, you can either `go install` the module directly, or run `make install-tools` in the root module directory.

## Add a New Tool

If you need to add a new tool, the easiest way is to add the tool to `internal/tools/tools.go`:
```go
import (
    ...
    _ "newmodule"
)
```
This file is a trick to make it so `go mod tidy` see these as direct dependencies and does not automatically remove them from the `go.mod` file for being "unused". After adding this import and running `go mod tidy` in `internal/tools`, this new import will be auto-added to `internal/tools/go.mod`.

Once you have the new tool installed, you'll need to add it to the install lists in the `install-tools` make target and `.build/Dockerfile` (in the Dockerfile you'll need to manually keep the versions in sync with `internal/tools`).  
While it's not technically required, it's also recommended to include a make target for future developers to conveniently use the new tool, and to add it to the `precommit` and `presubmit` targets if applicable.
