# Using the repo Makefile

The Makefile at the root of this repo provides numerous conveniences for developers working on this repo.

## Go Workspaces note

Depending on the state of your repo at any given time, you may need to provide `GOWORK=off` to any target. If you receive errors along the line of `main module does not include` or `unknown revision` it is likely that you need to provide this environment variable like so:
```
GOWORK=off make <target>
```
It is not recommended to export this variable permanently to your environment, as this makes Go workspaces not usable. They are very helpful for component development in this repo, since they are each individual modules.

## dev-setup

After first cloning the repo, it is recommended to run `make dev-setup`. This will:

* Install all necessary tools
* Set up Go Workspace for multi-module development
* Set up pre-commit hooks to block commits that don't meet requirements

Run `make dev-setup` to set these up. The make target `make precommit` is what the precommit hook will run, so you can run it manually if you would to check instead.

## addlicense

If `make checklicense` fails, you can fix it by running `make addlicense`. This is set up to include any files that need licenses according to the way the `addlicense` tool works.

(Currently this is missing support for a couple of files that don't have extensions, due to a limitation in `addlicense` around known extension types. We may have to contribute to the tool, as it does not have regular maintainers at the moment)

## update-otel-components

You can update all of the components within this repo to new versions of the collector by specifying `OTEL_VERSION` and `OTEL_CONTRIB_VERSION` variables, either in the environment when calling the target or by editing the `Makefile`. If you are doing a permanent update, the latter route is recommended.

`make update-otel-components` will do the following:
* Update all otel dependencies based on the specified versions. This will also use the [`otel_component_versions` script](../internal/tools/cmd/otel_component_versions) to detect which components require the stable version based on what is passed into `OTEL_VERSION`.
* Updates and reinstalls the `mdatagen` tool to the `OTEL_VERSION`
* Runs `make generate` in all component folders using the newly updated `mdatagen`

From this point, manual intervention will likely be required by the developer to ensure necessary API updates and generated code updates are used properly within our own code. You can use `make test-all` to verify changes are successful and that all components pass build and test after updates and manual fixes.

## gen-<distro>

Each distribution in this repo will have associated targets for using `distrogen` to generate updates.

The `gen-<distro>` targets are the default. They will generate without the `-force` flag, meaning if the spec file for the distro has not changed then generation will be skipped.
`regen-<distro>` will add the `-force` flag. This is generally used if you have made your own updates to the templates and want to do a generation while skipping the spec comparison step.
`regen-<distro>-v` will do a `-force` generation with debug logging for `distrogen` turned on.

## distrogen-golden-update

This target regenerates the `golden` files for all test cases in `distrogen`. You can add new testcases by following the format in `cmd/distrogen/testdata/generator` and running this target to generate the goldens for the new testcase.

## Updating dependencies

Since this is a multi-module repo, updating dependencies has to happen individually in each module. This is quite tedious to do manually, so there is a target in the Makefile to do this:

```
make update-go-module-in-all GO_MOD=golang.org/x/net GO_MOD_VERSION=v0.36.0
```
NOTE: You can omit `GO_MOD_VERSION` if you simply want to update the module to `latest`.

This target will run `update-go-module` from `maintenance.mk` in each component. If the requested module is not found, it moves on. Otherwise, it runs `go get -u` with the module and version specified.