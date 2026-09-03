copyfighter
===========

Copyfighter statically analyzes Go code and reports functions that are passing
large structs by value. It helps you help your code.

Every Go function call copies the values given to it, including structs. When
large structs are passed around without using a pointer to them, the copying of
new data in memory causes more allocations and more work for your garbage
collector.

Copyfighter's static analysis will identify where large structs, without
pointers, are being used as method receivers, function parameters and return
values.

Install with `go install github.com/jmhodges/copyfighter@latest`.

Point it at a package directory, an import path, or a `...` pattern, the same
way you would `go build` or `go vet`. It resolves packages with the go tool, so
it works in any module:

    $ copyfighter ./path/to/pkg
    $ copyfighter github.com/you/yourmodule/...
    $ copyfighter /some/other/checkout/pkg

Example output
---------------
    $ copyfighter path/to/pkg
    path/to/pkg/config.go:59:6: parameter 'c' at index 0 should be made into a pointer (func Configure(c config))
    path/to/pkg/config.go:63:17: receiver should be made into a pointer (func (config).Validate())

    $ copyfighter -max 32 path/to/pkg
    path/to/pkg/client.go:24:6: parameter 'f' at index 0 should be made into a pointer (func CallsFoo(f Foo))
    path/to/pkg/client.go:28:14: receiver should be made into a pointer (func (Foo).OnOtherToo(o other))
    path/to/pkg/config.go:59:6: parameter 'c' at index 0 should be made into a pointer (func Configure(c config))
    path/to/pkg/config.go:63:17: receiver should be made into a pointer (func (config).Validate())

Copyfighter exits with status 2 when it finds something to report.

Defaults And Flags
------------------

By default, copyfighter assumes structs wider than 64 bytes (eight words on
x86\_64) should not be copied. That's where the Go compiler stops copying a
struct inline and starts calling out to a copy routine, on both amd64 and
arm64. Smaller structs are cheap to copy and usually get passed in registers
anyway. Use the `-max` flag to change it. `max` should typically be set to some
multiple of the word size. You can also adjust the word size and alignment
offset for your preferred architecture with `-wordSize` and `-maxAlign`.

The defaults work as-is on 64-bit ARM. Same word size, same alignment, same
64 byte cutoff in the compiler. arm64 does hand out 16 integer registers for
arguments where amd64 has 9, so up to 128 bytes of ints and pointers can go
through a call without touching memory. If that's all you care about, `-max
128` is fine, but anything over 64 bytes still hits the copy routine every
time it's assigned or returned.

Flags like `-max` have to go before the package pattern.

FAQ
---

Why not just use Go's heap profiler to fix these problems when they show up?

Because copyfighter can find problems before you put your code in production. It's nice to prevent issues before they matter.
