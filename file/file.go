package file

import (
	"bytes"
	"fmt"
	"github.com/edsrzf/mmap-go"
	"github.com/juju/errgo"
	"io"
	"os"
	"sort"
)

// File manages access to objects stored in a PDF file.
type File struct {
	filename string
	file     *os.File
	mmap     mmap.MMap

	xrefs   map[Integer]crossReference // existing objects
	objects []IndirectObject           // new objects
	prev    Integer
	Trailer Dictionary
}

// Open opens a PDF file for manipulation of its objects.
func Open(filename string) (*File, error) {
	file := &File{
		filename: filename,
	}

	var err error
	file.file, err = os.Open(filename)
	if err != nil {
		return nil, errgo.Mask(err)
	}

	file.mmap, err = mmap.Map(file.file, mmap.RDONLY, 0)
	if err != nil {
		file.Close()
		return nil, errgo.Mask(err)
	}

	// check pdf file header
	if !bytes.Equal(file.mmap[:7], []byte("%PDF-1.")) {
		file.Close()
		return nil, errgo.New("file does not have PDF header")
	}

	err = file.loadReferences()
	if err != nil {
		file.Close()
		return nil, err
	}

	return file, nil
}

// Create creates a new PDF file with no objects.
func Create(filename string) (*File, error) {
	file := &File{
		filename: filename,
		Trailer:  Dictionary{},
	}

	// create enough of the pdf so that
	// appends will not break things
	f, err := os.Create(filename)
	if err != nil {
		return nil, errgo.Mask(err)
	}
	defer f.Close()
	f.Write([]byte("%PDF-1.7"))

	return file, nil
}

// Get returns the referenced object.
// When the object does not exist, Null is returned.
func (f *File) Get(reference ObjectReference) Object {
	for _, obj := range f.objects {
		if obj.ObjectNumber == reference.ObjectNumber && obj.GenerationNumber == reference.GenerationNumber {
			return obj
		}
	}

	xref, ok := f.xrefs[Integer(reference.ObjectNumber)]
	if !ok {
		return Null{}
	}

	switch xref[0] {
	case 0: // free entry
		return Null{}
	case 1: // normal
		offset := xref[1] - 1
		obj, _, err := parseIndirectObject(f.mmap[offset:])
		if err != nil {
			fmt.Println("file.Get:", err)
		}
		return obj.(IndirectObject).Object
	case 2: // in object stream
		panic("object streams not yet supported")
	default:
		panic(xref[0])
	}
}

// Add returns the of the object after adding it to the file.
// An IndirectObject's ObjectReference will be used,
// otherwise a free ObjectReference will be used.
func (f *File) Add(obj Object) ObjectReference {
	// TODO: handle non indirect-objects
	ref := ObjectReference{}

	switch typed := obj.(type) {
	case IndirectObject:
		ref.ObjectNumber = typed.ObjectNumber
		ref.GenerationNumber = typed.GenerationNumber
		f.objects = append(f.objects, typed)
	default:
		panic(obj)
	}
	return ref
}

func writeLineBreakTo(w io.Writer) (int64, error) {
	n, err := w.Write([]byte{'\n', '\n'})
	return int64(n), err
}

// Save appends the objects that have been added to the File
// to the file on disk. After saving, the File is still usable
// and will act as though it were just Open'ed.
//
// NOTE: A new object index will be written on each save,
// taking space in the file on disk
func (f *File) Save() error {
	info, err := os.Stat(f.filename)
	if err != nil {
		return errgo.Mask(err)
	}

	file, err := os.OpenFile(f.filename, os.O_RDWR|os.O_APPEND, 0666)
	if err != nil {
		return errgo.Mask(err)
	}
	defer file.Close()

	offset := info.Size() + 1

	n, err := writeLineBreakTo(file)
	if err != nil {
		return errgo.Mask(err)
	}
	offset += n

	xrefs := map[Integer]crossReference{}

	xrefs[0] = crossReference{0, 0, 65535}

	for i := range f.objects {
		// fmt.Println("writing object", i, "at", offset)
		xrefs[Integer(f.objects[i].ObjectNumber)] = crossReference{1, int(offset - 1), int(f.objects[i].GenerationNumber)}
		n, err = f.objects[i].WriteTo(file)
		if err != nil {
			return errgo.Mask(err)
		}
		offset += n

		n, err = writeLineBreakTo(file)
		if err != nil {
			return errgo.Mask(err)
		}
		offset += n
	}

	objects := make(sort.IntSlice, 0, len(xrefs))
	for objectNumber := range xrefs {
		objects = append(objects, int(objectNumber))
	}
	objects.Sort()

	// group into consecutive sets
	groups := []sort.IntSlice{}
	groupStart := 0
	for i := range objects {
		if i == 0 {
			continue
		}

		if objects[i] != objects[i-1]+1 {
			if groupStart+i-1 == groupStart {
				// handle single length groups
				groups = append(groups, objects[groupStart:groupStart+1])
			} else {
				groups = append(groups, objects[groupStart:groupStart+i-1])
			}
			groupStart = i
		}
	}
	// add remaining group
	groups = append(groups, objects[groupStart:])

	// write as an xref table to file
	fmt.Fprintf(file, "xref\n")
	for _, group := range groups {
		fmt.Fprintf(file, "%d %d\n", group[0], len(group))
		for _, objectNumber := range group {
			xref := xrefs[Integer(objectNumber)]
			fmt.Fprintf(file, "%010d %05d ", xref[1], xref[2])
			switch xref[0] {
			case 0:
				// f entries
				fmt.Fprintf(file, "f\n")
			case 1:
				// n entries
				fmt.Fprintf(file, "n\n")
			case 2:
				panic("can't be in xref table")
			default:
				panic("unhandled case")
			}
		}
	}

	// Write the file trailer
	fmt.Fprintf(file, "\ntrailer\n")
	trailer := Dictionary{}
	root, ok := f.Trailer[Name("Root")]
	if ok {
		trailer[Name("Root")] = root
	}

	// Figure out the highest object number to set Size properly
	maxObjNum := Integer(objects[len(objects)-1])
	for objNum := range f.xrefs {
		if objNum > maxObjNum {
			maxObjNum = objNum
		}
	}
	trailer[Name("Size")] = maxObjNum + 1

	if f.prev != 0 {
		trailer[Name("Prev")] = f.prev
	}

	_, err = trailer.WriteTo(file)
	if err != nil {
		return errgo.Mask(err)
	}

	fmt.Fprintf(file, "\nstartxref\n%d\n%%%%EOF", offset-1)

	return nil
}

// Close the File, does not Save.
func (f *File) Close() error {
	err := f.mmap.Unmap()
	if err != nil {
		return err
	}

	err = f.file.Close()
	if err != nil {
		return err
	}

	return nil
}
