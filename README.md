copyfighter
===========

Copyfighter reports Go functions that are passing large structs by value, instead
of by pointer, causing too much copying.

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
