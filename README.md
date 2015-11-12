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

Install with `go get` or similar.

Example output
---------------
    $ copyfighter path/to/pkg
    # parameter 'f' at index 0 should be made into a pointer
    func CallsFoo(f Foo)
    
    # receiver, and parameter 'o' at index 0 should be made into pointers
    func (Foo).OnOtherToo(o other)
    
    # receiver should be made into a pointer
    func (other).OnStruct()
    
    # receiver should be made into a pointer
    func (other).OnStruct2()


Defaults
--------
By default, it assumes structs wider than 16 bytes (two words on x86\_64) should
not be copied. This can be adjusted with the `-max` flag. `max` should typically
be set to some multiple of the word size.
