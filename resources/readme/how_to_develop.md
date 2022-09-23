## Prerequisites to develop
- Golang language - if you want to add go code
  - With homebrew `brew install go` (Recommended)
  - From [official website](https://go.dev)

## How to run in dev

If you want to add some functionality and test it, you should run in the root folder (requires GO installed).

```bash 
  go run .
```
This command will run cli, similar to `./corgi`, but all code changes will be visible in subsequent reruns. If you want to see changes in `./corgi`, after some code additions, that you need to [build applications](#how-to-build).

## How to build
To build the application and then run `./corgi` you need
```bash 
  go build
```

**Tip**: 
 - In order to automatically push `corgi` binary to git, you need to run `make fixPreCommitHooks` to make `pre-commit` hook executable



## FAQ
- -bash: ./pre-commit: /bin/bash: bad interpreter: Operation not permitted

For this you need to go to .githooks folder and run 
```
xattr -l pre-commit
```

If you will see in terminal `com.apple.quarantine`, than type
```
xattr -d com.apple.quarantine pre-commit
```
It should fix commit pre hook

</br>

[Main docs](../../README.md)