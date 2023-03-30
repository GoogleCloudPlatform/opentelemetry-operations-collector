# Using Go Build Tags

The codebase currently has two tags that affect what is used in builds:
* `windows` 
    - Used to signify that this is a Windows build, set automatically if GOOS=windows
* `gpu`
    - Used to control GPU receiver support

If you are using an editor that supports `gopls` like VSCode or GoLand, you may get confusing results with these tags not set. You can use `gopls` flags to set these tags. For example, in VSCode in `settings.json` (a local one in `.vscode` folder is recommended):
```json
{
    "gopls": {
        "build.buildFlags": [
            "-tags=windows,gpu"
        ]
    }
}
```
Other environments should have their own guides to pass build flags to `gopls`. (Feel free to add them here if you feel inclined :smile:)