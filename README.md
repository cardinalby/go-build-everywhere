# xgo-as-library

It's a copy of [crazy-max/xgo](https://github.com/crazy-max/xgo) go utility rewritten as a library
so that you can include it in your own go program.

## Usage

```go
import "github.com/cardinalby/xgo-as-library"

...

logger := log.New(os.Stdout, "", log.LstdFlags)
args := xgolib.Args{
	Repository: '.',
	SrcPackage: 'cmd/myapp',
	Targets: []string{"linux/amd64", "windows/amd64"},
}
if err := xgolib.StartBuild(args, logger); err != nil {
    log.Fatal(err)
}
```