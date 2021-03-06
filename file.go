package pdf

// NOTE for encryption:
// encrypt during Add
// decrypt during Get

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/edsrzf/mmap-go"
	"io"
	"os"
	"sort"
)

type freeObject uint // generation number for next use of the object number where this is stored

// File manages access to objects stored in a PDF file.
// Contains the non-managed keys from the file trailer.
type File struct {
	filename string
	file     *os.File
	mmap     mmap.MMap
	created  bool

	// cross reference for existing objects
	// indirect object for new objects
	// free object for newly freed objects
	// map key is the object number
	// make sure generation number is >= existing generation number when modifying
	objects map[uint]interface{}
	size    uint // max object number + 1

	prev Integer

	// The catalog dictionary for the PDF document contained in the file.
	Root ObjectReference

	// The Document's encryption dictionary
	Encrypt Dictionary

	// The document's information dictionary
	Info ObjectReference

	// An array of two byte-strings constituting a file identifier for the file.
	ID Array
}

// Open opens a PDF file for manipulation of its objects.
func Open(filename string) (*File, error) {
	file := &File{
		filename: filename,
		objects:  map[uint]interface{}{},
	}

	var err error
	file.file, err = os.Open(filename)
	if err != nil {
		return nil, err
	}

	file.mmap, err = mmap.Map(file.file, mmap.RDONLY, 0)
	if err != nil {
		err2 := file.Close()
		if err2 != nil {
			return nil, fmt.Errorf("%v %v", err, err2)
		}
		return nil, err
	}

	// check pdf file header
	if !bytes.Equal(file.mmap[:7], []byte("%PDF-1.")) {
		err := file.Close()
		if err != nil {
			return nil, errors.New("file does not have PDF header; " + err.Error())
		}
		return nil, errors.New("file does not have PDF header")
	}

	err = file.loadReferences()
	if err != nil {
		err2 := file.Close()
		if err2 != nil {
			return nil, fmt.Errorf("%v %v", err, err2)
		}
		return nil, err
	}

	return file, nil
}

// Create creates a new PDF file with no objects.
func Create(filename string) (*File, error) {
	file := &File{
		filename: filename,
		objects:  map[uint]interface{}{},
		created:  true,
		size:     1,
	}

	// create enough of the pdf so that
	// appends will not break things
	f, err := os.Create(filename)
	if err != nil {
		return nil, err
	}
	defer func() {
		err := f.Close()
		if err != nil {
			panic(err)
		}
	}()

	_, err = f.Write([]byte("%PDF-1.7"))
	if err != nil {
		return nil, err
	}

	return file, nil
}

// Get returns the referenced object.
// When the object does not exist, Null is returned.
func (f *File) Get(ref ObjectReference) Object {
	// fmt.Println("getting: ", ref)
	objectRaw, ok := f.objects[ref.ObjectNumber]
	if !ok {
		return Null{fmt.Errorf("%s not found", ref)}
	}

	var object Object

	switch typed := objectRaw.(type) {
	case crossReference: // existing object
		switch typed[0] {
		case 0: // free entry
			return Null{fmt.Errorf("%s is a free object", ref)}
		case 1: // normal
			offset := typed[1] - 1
			obj, _, err := parseIndirectObject(f.mmap[offset:])
			if err != nil {
				return Null{fmt.Errorf("Error parsing %s", ref)}
			}

			iobj, ok := obj.(IndirectObject)
			if !ok {
				return Null{fmt.Errorf("%v is not an indirect object", ref)}
			}

			if iobj.Object == nil {
				return Null{fmt.Errorf("%v's object is nil", ref)}
			}
			object = iobj.Object
		case 2: // in object stream
			// get the object stream
			objectStreamRef := ObjectReference{ObjectNumber: typed[1]}
			objectStream, ok := f.Get(objectStreamRef).(Stream)
			if !ok {
				return Null{fmt.Errorf("%v should be in object stream %v, but %v is not a stream", ref, objectStreamRef, objectStreamRef)}
			}

			// parse the index (object number and offset pairs)
			index := []Integer{}
			N := int(objectStream.Dictionary[Name("N")].(Integer))
			stream, err := objectStream.Decode()
			if err != nil {
				return Null{fmt.Errorf("could not decode %v", objectStreamRef)}
			}

			offset := 0
			for i := 0; i < N*2; i++ {
				obj, n, err := parseNumeric(stream[offset:])
				if err != nil {
					return Null{fmt.Errorf("unable to parse numeric %v", stream[offset:])}
				}

				index = append(index, obj.(Integer))
				offset += n
			}

			// find the offset for the object we are looking for
			start := typed[2] * 2
			objectNumber := index[start]
			offset = int(index[start+1])

			// if the index from the cross reference is wrong,
			// find the correct offset
			if objectNumber != Integer(ref.ObjectNumber) {
				objectNumber = Integer(ref.ObjectNumber)
				for i := 0; i < len(index); i += 2 {
					if index[i] == objectNumber {
						offset = int(index[i+1])
						break
					}
				}
			}

			// grab the object
			first := int(objectStream.Dictionary[Name("First")].(Integer))
			object, _, err = parseObject(stream[first+offset:])
			if err != nil {
				return Null{fmt.Errorf("unable to parse object %v", stream[first+offset:])}
			}
		default:
			panic(typed[0])
		}
	case IndirectObject: // new object
		if typed.Object == nil {
			fmt.Println("+++++++++++++++++indirect object's object is nil")
		}
		object = typed.Object
	case freeObject: // newly freed object
		return Null{fmt.Errorf("%v freed after pdf was loaded", ref)}
	default:
		panic(fmt.Sprintf("unhandled type: %T", object))
	}

	// deal with streams that have refs to lengths
	if streamObj, ok := object.(Stream); ok {
		if lengthRef, ok := streamObj.Dictionary["Length"].(ObjectReference); ok {
			length := f.Get(lengthRef).(Integer)
			streamObj.Dictionary["Length"] = length
			streamObj.Stream = streamObj.Stream[:int(length)]
		}
		object = streamObj
	}

	return object
}

// Add returns the object reference of the object after adding it to the file.
// An IndirectObject's ObjectReference will be used,
// otherwise a free ObjectReference will be used.
//
// If an IndirectObject's ObjectReference also refers to an existing
// object, the newly added IndirectObject will mask the existing one.
// Only the most recently added object will be Saved to disk.
// GenerationNumber must be greater than or equal to the largest existing
// GenerationNumber for that ObjectNumber.
func (f *File) Add(obj Object) (ObjectReference, error) {
	// TODO: handle non indirect-objects
	ref := ObjectReference{}

	switch typed := obj.(type) {
	case IndirectObject:
		ref.ObjectNumber = typed.ObjectNumber
		ref.GenerationNumber = typed.GenerationNumber
		// fmt.Println("adding:", ref)

		// check to see if the generation number works
		existing, ok := f.objects[ref.ObjectNumber]
		if ok {
			// determine the minimum allowed generation number
			var minGenerationNumber uint
			switch typed := existing.(type) {
			case crossReference: // existing object
				switch typed[0] {
				case 0: // free entry
					minGenerationNumber = typed[2]
				case 1: // normal
					minGenerationNumber = typed[2]
				case 2: // in object stream
					// objects in object streams must have a
					// generation number of 0
					minGenerationNumber = 0
				default:
					panic(typed[0])
				}
			case IndirectObject: // new object
				minGenerationNumber = typed.GenerationNumber
			case freeObject: // newly freed object
				minGenerationNumber = uint(typed)
			default:
				panic(fmt.Sprintf("unhandled type: %T", typed))
			}

			if ref.GenerationNumber < minGenerationNumber {
				// TODO: make better error
				ref.GenerationNumber = minGenerationNumber
				return ref, errors.New("Generation number is too small...")
			}
		}

		f.objects[ref.ObjectNumber] = typed
	default:
		// TODO: reuse free object numbers
		objectNumber := f.size
		f.size++

		ref.ObjectNumber = objectNumber

		f.objects[objectNumber] = IndirectObject{
			ObjectReference: ref,
			Object:          obj,
		}

		// panic(obj)
	}
	return ref, nil
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
	// return f.saveUsingXrefTable()
	return f.saveUsingXrefStream()
}

func (f *File) saveUsingXrefTable() error {
	info, err := os.Stat(f.filename)
	if err != nil {
		return err
	}

	file, err := os.OpenFile(f.filename, os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return err
	}
	defer func() {
		err := file.Close()
		if err != nil {
			panic(err)
		}
	}()

	offset := info.Size() + 1

	n, err := writeLineBreakTo(file)
	if err != nil {
		return err
	}
	offset += n

	xrefs := map[Integer]crossReference{}

	xrefs[0] = crossReference{0, 0, 65535}

	free := sort.IntSlice{}
	for i := range f.objects {
		switch typed := f.objects[i].(type) {
		case crossReference:
			// no-op, don't need to write unchanged objects to file
			// however, we do need to handle the free list
			// xrefs[Integer(i)] = typed
			if typed[0] == 0 {
				free = append(free, int(i))
			}
		case IndirectObject:
			xrefs[Integer(i)] = crossReference{1, uint(offset - 1), typed.GenerationNumber}
			n, err = typed.writeTo(file)
			if err != nil {
				return err
			}
			offset += n

			n, err = writeLineBreakTo(file)
			if err != nil {
				return err
			}
			offset += n
		case freeObject:
			xrefs[Integer(i)] = crossReference{0, 0, uint(typed)}
			free = append(free, int(i))
		default:
			panic(fmt.Sprintf("unhandled type: %T", typed))
		}
	}

	// fill in the free linked list
	free.Sort()
	for i := 0; i < free.Len()-1; i++ {
		xref := xrefs[Integer(free[i])]
		xref[1] = uint(free[i+1])
		xrefs[Integer(free[i])] = xref
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
			groups = append(groups, objects[groupStart:i])
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
				fmt.Fprintf(file, "f\r\n")
			case 1:
				// n entries
				fmt.Fprintf(file, "n\r\n")
			case 2:
				panic("can't be in xref table")
			default:
				panic("unhandled case")
			}
		}
	}

	// Setup create the trailer
	fmt.Fprintf(file, "\ntrailer\n")
	trailer := Dictionary{}

	// Size
	// Figure out the highest object number to set Size properly
	var maxObjNum uint
	for objNum := range f.objects {
		if objNum > maxObjNum {
			maxObjNum = objNum
		}
	}
	trailer[Name("Size")] = Integer(maxObjNum + 1)

	// Prev
	if f.prev != 0 {
		trailer[Name("Prev")] = f.prev
	}

	// Root
	trailer[Name("Root")] = f.Root

	// Encrypt
	if len(f.Encrypt) != 0 {
		trailer[Name("Encrypt")] = f.Encrypt
	}

	// Info
	if f.Info.ObjectNumber != 0 {
		trailer[Name("Info")] = f.Info
	}

	// ID
	if len(f.ID) != 0 {
		trailer[Name("ID")] = f.ID
	}

	_, err = trailer.writeTo(file)
	if err != nil {
		return err
	}

	fmt.Fprintf(file, "\nstartxref\n%d\n%%%%EOF", offset-1)

	return nil
}

func (f *File) saveUsingXrefStream() error {
	info, err := os.Stat(f.filename)
	if err != nil {
		return err
	}

	file, err := os.OpenFile(f.filename, os.O_RDWR|os.O_APPEND, 0666)
	if err != nil {
		return err
	}
	defer func() {
		err := file.Close()
		if err != nil {
			panic(err)
		}
	}()

	offset := info.Size() + 1

	n, err := writeLineBreakTo(file)
	if err != nil {
		return err
	}
	offset += n

	xrefs := map[Integer]crossReference{}

	xrefs[0] = crossReference{0, 0, 65535}

	free := sort.IntSlice{0}
	for i := range f.objects {
		switch typed := f.objects[i].(type) {
		case crossReference:
			// no-op, don't need to write unchanged objects to file
			// however, we do need to handle the free list
			// xrefs[Integer(i)] = typed
			if typed[0] == 0 {
				free = append(free, int(i))
			}
		case IndirectObject:
			xrefs[Integer(i)] = crossReference{1, uint(offset - 1), typed.GenerationNumber}
			n, err = typed.writeTo(file)
			if err != nil {
				return err
			}
			offset += n

			n, err = writeLineBreakTo(file)
			if err != nil {
				return err
			}
			offset += n
		case freeObject:
			xrefs[Integer(i)] = crossReference{0, 0, uint(typed)}
			free = append(free, int(i))
		default:
			panic(fmt.Sprintf("unhandled type: %T", typed))
		}
	}

	// Figure out the highest object number to set Size properly
	var maxObjNum uint
	for objNum := range f.objects {
		if objNum > maxObjNum {
			maxObjNum = objNum
		}
	}

	// add an xref for the xrefstream
	xrefstreamObjectNumber := uint(maxObjNum + 1)
	maxObjNum++
	xref := crossReference{1, uint(offset - 1), 0}
	xrefs[Integer(xrefstreamObjectNumber)] = xref
	f.objects[xrefstreamObjectNumber] = xref

	// fill in the free linked list
	free.Sort()
	for i := 0; i < free.Len()-1; i++ {
		xref := xrefs[Integer(free[i])]
		xref[1] = uint(free[i+1])
		xrefs[Integer(free[i])] = xref
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
			groups = append(groups, objects[groupStart:i])
			groupStart = i
		}
	}
	groups = append(groups, objects[groupStart:])

	// Create the xrefstream dictionary (the trailer)
	trailer := Dictionary{}
	trailer[Name("Size")] = Integer(maxObjNum + 1)

	// Prev
	if f.prev != 0 {
		trailer[Name("Prev")] = f.prev
	}

	// Root
	trailer[Name("Root")] = f.Root

	// Encrypt
	if len(f.Encrypt) != 0 {
		trailer[Name("Encrypt")] = f.Encrypt
	}

	// Info
	if f.Info.ObjectNumber != 0 {
		trailer[Name("Info")] = f.Info
	}

	// ID
	if len(f.ID) != 0 {
		trailer[Name("ID")] = f.ID
	}

	// Add xrefstream specific things to trailer
	trailer["Type"] = Name("XRef")

	// Index
	index := Array{}
	// fmt.Println(groups)
	for _, group := range groups {
		index = append(index, Integer(group[0]), Integer(len(group)))
	}
	trailer["Index"] = index

	// layout for the stream (W)
	maxXref := [3]uint{}
	for _, xref := range xrefs {
		for i := 0; i < len(xref); i++ {
			if xref[i] > maxXref[i] {
				maxXref[i] = xref[i]
			}
		}
	}
	nBytes := [3]int{}
	for i := range nBytes {
		nBytes[i] = nBytesForInt(int(maxXref[i]))
	}
	trailer["W"] = Array{Integer(nBytes[0]), Integer(nBytes[1]), Integer(nBytes[2])}

	// log.Println(xrefs)

	stream := &bytes.Buffer{}
	for _, group := range groups {
		for _, objectNumber := range group {
			xref := xrefs[Integer(objectNumber)]
			for i := range xref {
				_, err = stream.Write(intToBytes(xref[i], nBytes[i]))
				if err != nil {
					return err
				}
			}
		}
	}

	xrefstream := IndirectObject{
		ObjectReference: ObjectReference{
			ObjectNumber: xrefstreamObjectNumber,
		},
		Object: Stream{
			Dictionary: trailer,
			Stream:     stream.Bytes(),
		},
	}
	_, err = f.Add(xrefstream)
	if err != nil {
		return err
	}

	_, err = xrefstream.writeTo(file)
	if err != nil {
		return err
	}

	fmt.Fprintf(file, "\nstartxref\n%d\n%%%%EOF", offset-1)

	return nil
}

// Close the File, does not Save.
func (f *File) Close() error {
	if f.created {
		// don't need to clean up mmap
		return nil
	}

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

// Free the object with the specified number.
// Will automatically determine and increment the generation number.
func (f *File) Free(objectNumber uint) {
	obj, ok := f.objects[objectNumber]
	if !ok {
		// object does not exist, and therefore is already free
		return
	}

	switch typed := obj.(type) {
	case crossReference: // existing object
		switch typed[0] {
		case 0: // free entry
			// no-op
			// the object is already free
		case 1: // normal
			f.objects[objectNumber] = freeObject(typed[2] + 1)
		case 2: // in object stream
			// objects in object streams must have a
			// generation number of 0
			f.objects[objectNumber] = freeObject(1)
		default:
			panic(typed[0])
		}
	case IndirectObject: // new object
		f.objects[objectNumber] = freeObject(typed.GenerationNumber + 1)
	case freeObject: // newly freed object
		// no-op
		// already free
	default:
		panic(fmt.Sprintf("unhandled type: %T", typed))
	}
}
