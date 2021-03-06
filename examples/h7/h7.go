package main

import (
	"github.com/juju/errgo"
	"github.com/nathankerr/pdf"
	"log"
)

func main() {
	log.SetFlags(log.Lshortfile)

	createMinimalFile()
	stage1()
	stage2()
	stage3()
	stage4()
	log.Printf("done")
}

// create the minimal file described in H.2
func createMinimalFile() {
	log.Printf("createMinimalFile")

	minimal, err := pdf.Create("h7-minimal.pdf")
	if err != nil {
		log.Fatalln(errgo.Details(err))
	}
	defer minimal.Close()

	minimal.Root = pdf.ObjectReference{ObjectNumber: 1}

	// catalog
	minimal.Add(pdf.IndirectObject{
		ObjectReference: pdf.ObjectReference{ObjectNumber: 1},
		Object: pdf.Dictionary{
			pdf.Name("Type"): pdf.Name("Catalog"),
			pdf.Name("Outlines"): pdf.ObjectReference{
				ObjectNumber: 2,
			},
			pdf.Name("Pages"): pdf.ObjectReference{
				ObjectNumber: 3,
			},
		},
	})

	// outlines
	minimal.Add(pdf.IndirectObject{
		ObjectReference: pdf.ObjectReference{ObjectNumber: 2},
		Object: pdf.Dictionary{
			pdf.Name("Type"):  pdf.Name("Outlines"),
			pdf.Name("Count"): pdf.Integer(0),
		},
	})

	// pages
	minimal.Add(pdf.IndirectObject{
		ObjectReference: pdf.ObjectReference{ObjectNumber: 3},
		Object: pdf.Dictionary{
			pdf.Name("Type"): pdf.Name("Pages"),
			pdf.Name("Kids"): pdf.Array{
				pdf.ObjectReference{
					ObjectNumber: 4,
				},
			},
			pdf.Name("Count"): pdf.Integer(1),
		},
	})

	// page
	minimal.Add(pdf.IndirectObject{
		ObjectReference: pdf.ObjectReference{ObjectNumber: 4},
		Object: pdf.Dictionary{
			pdf.Name("Type"): pdf.Name("Page"),
			pdf.Name("Parent"): pdf.ObjectReference{
				ObjectNumber: 3,
			},
			pdf.Name("MediaBox"): pdf.Array{
				pdf.Integer(0),
				pdf.Integer(0),
				pdf.Integer(612),
				pdf.Integer(792),
			},
			pdf.Name("Contents"): pdf.ObjectReference{
				ObjectNumber: 5,
			},
			pdf.Name("Resources"): pdf.Dictionary{
				pdf.Name("ProcSet"): pdf.ObjectReference{
					ObjectNumber: 6,
				},
			},
		},
	})

	// content stream
	minimal.Add(pdf.IndirectObject{
		ObjectReference: pdf.ObjectReference{ObjectNumber: 5},
		Object: pdf.Stream{
			Dictionary: pdf.Dictionary{
				pdf.Name("Length"): pdf.Integer(0),
			},
		},
	})

	// procset
	minimal.Add(pdf.IndirectObject{
		ObjectReference: pdf.ObjectReference{ObjectNumber: 6},
		Object: pdf.Array{
			pdf.Name("PDF"),
		},
	})

	minimal.Root = pdf.ObjectReference{ObjectNumber: 1}

	err = minimal.Save()
	if err != nil {
		log.Fatalln(errgo.Details(err))
	}
}

// Stage 1: Add Four Text Annotations
func stage1() {
	log.Println("stage 1")

	minimal, err := pdf.Open("h7-minimal.pdf")
	if err != nil {
		log.Fatalln(errgo.Details(err))
	}

	// page
	page := minimal.Get(pdf.ObjectReference{ObjectNumber: 4}).(pdf.Dictionary)
	page[pdf.Name("Annots")] = pdf.ObjectReference{ObjectNumber: 7}
	minimal.Add(pdf.IndirectObject{
		ObjectReference: pdf.ObjectReference{ObjectNumber: 4},
		Object:          page,
	})

	// annotation array
	minimal.Add(pdf.IndirectObject{
		ObjectReference: pdf.ObjectReference{ObjectNumber: 7},
		Object: pdf.Array{
			pdf.ObjectReference{ObjectNumber: 8},
			pdf.ObjectReference{ObjectNumber: 9},
			pdf.ObjectReference{ObjectNumber: 10},
			pdf.ObjectReference{ObjectNumber: 11},
		},
	})

	// annotation
	minimal.Add(pdf.IndirectObject{
		ObjectReference: pdf.ObjectReference{ObjectNumber: 8},
		Object: pdf.Dictionary{
			pdf.Name("Type"):    pdf.Name("Annot"),
			pdf.Name("Subtype"): pdf.Name("Text"),
			pdf.Name("Rect"): pdf.Array{
				pdf.Integer(44),
				pdf.Integer(616),
				pdf.Integer(162),
				pdf.Integer(735),
			},
			pdf.Name("Contents"): pdf.String("Text #1"),
			pdf.Name("Open"):     pdf.Boolean(true),
		},
	})

	// annotation
	minimal.Add(pdf.IndirectObject{
		ObjectReference: pdf.ObjectReference{ObjectNumber: 9},
		Object: pdf.Dictionary{
			pdf.Name("Type"):    pdf.Name("Annot"),
			pdf.Name("Subtype"): pdf.Name("Text"),
			pdf.Name("Rect"): pdf.Array{
				pdf.Integer(224),
				pdf.Integer(668),
				pdf.Integer(457),
				pdf.Integer(735),
			},
			pdf.Name("Contents"): pdf.String("Text #2"),
			pdf.Name("Open"):     pdf.Boolean(false),
		},
	})

	// annotation
	minimal.Add(pdf.IndirectObject{
		ObjectReference: pdf.ObjectReference{ObjectNumber: 10},
		Object: pdf.Dictionary{
			pdf.Name("Type"):    pdf.Name("Annot"),
			pdf.Name("Subtype"): pdf.Name("Text"),
			pdf.Name("Rect"): pdf.Array{
				pdf.Integer(239),
				pdf.Integer(393),
				pdf.Integer(328),
				pdf.Integer(622),
			},
			pdf.Name("Contents"): pdf.String("Text #3"),
			pdf.Name("Open"):     pdf.Boolean(true),
		},
	})

	// annotation
	minimal.Add(pdf.IndirectObject{
		ObjectReference: pdf.ObjectReference{ObjectNumber: 11},
		Object: pdf.Dictionary{
			pdf.Name("Type"):    pdf.Name("Annot"),
			pdf.Name("Subtype"): pdf.Name("Text"),
			pdf.Name("Rect"): pdf.Array{
				pdf.Integer(34),
				pdf.Integer(398),
				pdf.Integer(225),
				pdf.Integer(575),
			},
			pdf.Name("Contents"): pdf.String("Text #4"),
			pdf.Name("Open"):     pdf.Boolean(false),
		},
	})

	err = minimal.Save()
	if err != nil {
		log.Fatalln(errgo.Details(err))
	}
}

// Stage 2: Modify Text of One Annotation
func stage2() {
	log.Println("stage 2")

	minimal, err := pdf.Open("h7-minimal.pdf")
	if err != nil {
		log.Fatalln(errgo.Details(err))
	}

	annotation := minimal.Get(pdf.ObjectReference{ObjectNumber: 10}).(pdf.Dictionary)
	annotation[pdf.Name("Contents")] = pdf.String("Modified Text #3")
	minimal.Add(pdf.IndirectObject{
		ObjectReference: pdf.ObjectReference{ObjectNumber: 10},
		Object:          annotation,
	})

	err = minimal.Save()
	if err != nil {
		log.Fatalln(errgo.Details(err))
	}
}

// Stage 3: Delete Two Annotations
func stage3() {
	log.Println("stage 3")

	minimal, err := pdf.Open("h7-minimal.pdf")
	if err != nil {
		log.Fatalln(errgo.Details(err))
	}

	// log.Println("minimal:", minimal)

	// annotation array
	minimal.Add(pdf.IndirectObject{
		ObjectReference: pdf.ObjectReference{ObjectNumber: 7},
		Object: pdf.Array{
			pdf.ObjectReference{ObjectNumber: 10},
			pdf.ObjectReference{ObjectNumber: 11},
		},
	})

	minimal.Free(8)
	minimal.Free(9)

	err = minimal.Save()
	if err != nil {
		log.Fatalln(errgo.Details(err))
	}
}

// Stage 4: Add Three Annotations
func stage4() {
	log.Println("stage 4")

	minimal, err := pdf.Open("h7-minimal.pdf")
	if err != nil {
		log.Fatalln(errgo.Details(err))
	}

	annotationsObj := minimal.Get(pdf.ObjectReference{ObjectNumber: 7})
	annotations := annotationsObj.(pdf.Array)

	// annotation 8 1
	ref, err := minimal.Add(pdf.IndirectObject{
		ObjectReference: pdf.ObjectReference{ObjectNumber: 8, GenerationNumber: 1},
		Object: pdf.Dictionary{
			pdf.Name("Type"):    pdf.Name("Annot"),
			pdf.Name("Subtype"): pdf.Name("Text"),
			pdf.Name("Rect"): pdf.Array{
				pdf.Integer(58),
				pdf.Integer(657),
				pdf.Integer(172),
				pdf.Integer(742),
			},
			pdf.Name("Contents"): pdf.String("New Text #1"),
			pdf.Name("Open"):     pdf.Boolean(true),
		},
	})
	if err != nil {
		log.Fatal(errgo.Details(err))
	}
	annotations = append(annotations, ref)

	// annotation 9 1
	ref, err = minimal.Add(pdf.IndirectObject{
		ObjectReference: pdf.ObjectReference{ObjectNumber: 9, GenerationNumber: 1},
		Object: pdf.Dictionary{
			pdf.Name("Type"):    pdf.Name("Annot"),
			pdf.Name("Subtype"): pdf.Name("Text"),
			pdf.Name("Rect"): pdf.Array{
				pdf.Integer(389),
				pdf.Integer(459),
				pdf.Integer(570),
				pdf.Integer(537),
			},
			pdf.Name("Contents"): pdf.String("New Text #2"),
			pdf.Name("Open"):     pdf.Boolean(false),
		},
	})
	if err != nil {
		log.Fatal(errgo.Details(err))
	}
	annotations = append(annotations, ref)

	// annotation 12 0
	ref, err = minimal.Add(pdf.IndirectObject{
		ObjectReference: pdf.ObjectReference{ObjectNumber: 12},
		Object: pdf.Dictionary{
			pdf.Name("Type"):    pdf.Name("Annot"),
			pdf.Name("Subtype"): pdf.Name("Text"),
			pdf.Name("Rect"): pdf.Array{
				pdf.Integer(44),
				pdf.Integer(253),
				pdf.Integer(473),
				pdf.Integer(337),
			},
			pdf.Name("Contents"): pdf.String("New Text #3\\203a longer text annotation which we will continue \\\nonto a second line"),
			pdf.Name("Open"):     pdf.Boolean(true),
		},
	})
	if err != nil {
		log.Fatal(errgo.Details(err))
	}
	annotations = append(annotations, ref)

	// update the annotations array
	minimal.Add(pdf.IndirectObject{
		ObjectReference: pdf.ObjectReference{ObjectNumber: 7},
		Object:          annotations,
	})

	err = minimal.Save()
	if err != nil {
		log.Fatalln(errgo.Details(err))
	}
}
