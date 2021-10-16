// Package main implements a Wayland protocol code generator.

package main

import (
	"bytes"
	"encoding/xml"
	"flag"
	"fmt"
	"go/format"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// The following datatypes map Wayland protocol schemas into Go structs.

type protocol struct {
	XMLName xml.Name `xml:"protocol"`
	Name    string   `xml:"name,attr"`

	Copyright  string  `xml:"copyright"`
	Interfaces []iface `xml:"interface"`
}

type iface struct {
	XMLName xml.Name `xml:"interface"`
	Name    string   `xml:"name,attr"`
	Version int      `xml:"version,attr"`

	Description description `xml:"description"`
	Enums       []enum      `xml:"enum"`
	Requests    []request   `xml:"request"`
	Events      []event     `xml:"event"`
}

type enum struct {
	XMLName  xml.Name `xml:"enum"`
	Name     string   `xml:"name,attr"`
	Bitfield bool     `xml:"bitfield,attr,omitempty"`

	Description description `xml:"description"`
	Entries     []entry     `xml:"entry"`
}

type entry struct {
	XMLName xml.Name `xml:"entry"`
	Name    string   `xml:"name,attr"`
	Value   string   `xml:"value,attr"`
	Summary string   `xml:"summary,attr"`
}

type request struct {
	XMLName xml.Name `xml:"request"`
	Name    string   `xml:"name,attr"`

	Description description `xml:"description"`
	Args        []arg       `xml:"arg"`
}

type event struct {
	XMLName xml.Name `xml:"event"`
	Name    string   `xml:"name,attr"`
	Since   int      `xml:"since,attr,omitempty"`

	Description description `xml:"description"`
	Args        []arg       `xml:"arg"`
}

type arg struct {
	XMLName   xml.Name `xml:"arg"`
	Name      string   `xml:"name,attr"`
	Type      string   `xml:"type,attr"`
	Interface string   `xml:"interface,attr,omitempty"`
	Summary   string   `xml:"summary,attr,omitempty"`
}

type description struct {
	XMLName xml.Name `xml:"description"`
	Summary string   `xml:"summary,attr"`

	Text string `xml:",chardata"`
}

type protocols []protocol

func (p protocols) Len() int {
	return len(p)
}

func (p protocols) Less(i, j int) bool {
	return p[i].Name < p[j].Name
}

func (p protocols) Swap(i, j int) {
	tmp := p[i]
	p[i] = p[j]
	p[j] = tmp
}

// Map of all known protocols. This is populated during the scanning phase.
var protos = protocols{}

func main() {
	flag.Parse()

	// Recursively scan each path provided on the command line.
	for _, arg := range flag.Args() {
		if err := walkdir(arg); err != nil {
			log.Printf("Error: parsing protocols: %v", err)
			os.Exit(1)
		}
	}

	// Sort protocols alphabetically.
	sort.Sort(protos)

	// Generate code to buffer
	buf := bytes.Buffer{}
	if err := codegen(&buf); err != nil {
		log.Printf("Error: generating code: %v", err)
	}

	// Format code
	b, err := format.Source(buf.Bytes())
	if err != nil {
		log.Printf("Error: formatting code: %v", err)
		b = buf.Bytes()
	}

	// Generate an output file containing all of our protocol data.
	if err := os.WriteFile("waylandproto_gen.go", b, 0644); err != nil {
		log.Printf("Error: creating output file: %v", err)
	}
}

func walkdir(path string) error {
	return filepath.Walk(path, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		name := info.Name()

		// Skip dotfiles.
		if name == "" || name[0] == '.' {
			if info.IsDir() {
				return filepath.SkipDir
			} else {
				return nil
			}
		}

		// Descend into directories.
		if info.IsDir() {
			return nil
		}

		// Skip non-XML files.
		if !strings.HasSuffix(name, ".xml") {
			return nil
		}

		// Skip tests.xml.
		if name == "tests.xml" {
			return nil
		}

		// Skip old xdg_shell.
		if name == "xdg-shell-unstable-v5.xml" || name == "xdg-shell-unstable-v6.xml" {
			return nil
		}

		if err := parsefile(path); err != nil {
			return fmt.Errorf("processing %q: %w", name, err)
		}

		return nil
	})
}

func parsefile(filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("opening file %q for reading: %w", filename, err)
	}

	defer func() {
		if err := f.Close(); err != nil {
			log.Printf("Warning: error closing %q: %v", filename, err)
		}
	}()

	protocol := protocol{}
	if err := xml.NewDecoder(f).Decode(&protocol); err != nil {
		return fmt.Errorf("parsing xml in %q: %w", filename, err)
	}

	protos = append(protos, protocol)

	return nil
}

func codegen(w io.Writer) error {
	// Output a preamble containing a comment explaining that the code is generated.
	args := strings.Join(os.Args[1:], " ")
	if _, err := fmt.Fprintf(w, "// THIS FILE IS GENERATED BY WAYGEN - DO NOT EDIT\n// Generated with: waygen %s\npackage wayland\n\n", args); err != nil {
		return fmt.Errorf("writing preamble: %w", err)
	}

	if _, err := fmt.Fprintf(w, "////////////////////////////////////////////////////////////////////////////////\n// Protocol Map\nvar Protocols = map[string]ProtocolDescriptor{\n"); err != nil {
		return fmt.Errorf("writing protocol map header: %v", err)
	}

	for _, proto := range protos {
		if _, err := fmt.Fprintf(w, "\t%q: {\n", proto.Name); err != nil {
			return fmt.Errorf("writing protocol map entry %q header: %v", proto.Name, err)
		}
		if _, err := fmt.Fprintf(w, "\t\tName: %q,\n", proto.Name); err != nil {
			return fmt.Errorf("writing protocol map entry %q name value: %v", proto.Name, err)
		}
		if _, err := fmt.Fprintf(w, "\t\tInterfaces: []InterfaceDescriptor{\n"); err != nil {
			return fmt.Errorf("writing protocol map entry %q interface list header: %v", proto.Name, err)
		}
		for _, intf := range proto.Interfaces {
			if _, err := fmt.Fprintf(w, "\t\t\t{\n\t\t\t\tName: %q,\n", intf.Name); err != nil {
				return fmt.Errorf("writing protocol map entry %q interface %q entry header: %v", proto.Name, intf.Name, err)
			}

			if _, err := fmt.Fprintf(w, "\t\t\t\tEvents: []EventDescriptor{\n"); err != nil {
				return fmt.Errorf("writing protocol map entry %q interface %q entry events header: %v", proto.Name, intf.Name, err)
			}
			for opcode, event := range intf.Events {
				if _, err := fmt.Fprintf(w, "\t\t\t\t\t{Name: %q, Opcode: %d, Type: &%s{}},\n", event.Name, opcode, namegen(intf.Name, event.Name, "event")); err != nil {
					return fmt.Errorf("writing protocol map entry %q interface %q event %q entry: %v", proto.Name, intf.Name, event.Name, err)
				}
			}
			if _, err := fmt.Fprintf(w, "\t\t\t\t},\n"); err != nil {
				return fmt.Errorf("writing protocol map entry %q interface %q entry events footer: %v", proto.Name, intf.Name, err)
			}

			if _, err := fmt.Fprintf(w, "\t\t\t\tRequests: []RequestDescriptor{\n"); err != nil {
				return fmt.Errorf("writing protocol map entry %q interface %q entry requests header: %v", proto.Name, intf.Name, err)
			}
			for opcode, request := range intf.Requests {
				if _, err := fmt.Fprintf(w, "\t\t\t\t\t{Name: %q, Opcode: %d, Type: &%s{}},\n", request.Name, opcode, namegen(intf.Name, request.Name, "request")); err != nil {
					return fmt.Errorf("writing protocol map entry %q interface %q request %q entry: %v", proto.Name, intf.Name, request.Name, err)
				}
			}
			if _, err := fmt.Fprintf(w, "\t\t\t\t},\n"); err != nil {
				return fmt.Errorf("writing protocol map entry %q interface %q entry requests footer: %v", proto.Name, intf.Name, err)
			}

			if _, err := fmt.Fprintf(w, "\t\t\t},\n"); err != nil {
				return fmt.Errorf("writing protocol map entry %q interface %q entry footer: %v", proto.Name, intf.Name, err)
			}
		}
		if _, err := fmt.Fprintf(w, "\t\t},\n"); err != nil {
			return fmt.Errorf("writing protocol map entry %q interface list header: %v", proto.Name, err)
		}
		if _, err := fmt.Fprintf(w, "\t},\n"); err != nil {
			return fmt.Errorf("writing protocol map entry %q footer: %v", proto.Name, err)
		}
	}

	if _, err := fmt.Fprintf(w, "}\n\n"); err != nil {
		return fmt.Errorf("writing protocol map footer: %v", err)
	}

	for _, proto := range protos {
		if err := codegenproto(w, proto); err != nil {
			return fmt.Errorf("generating code for proto %v: %w", proto.Name, err)
		}
	}

	return nil
}

var spacesRE = regexp.MustCompile(`\s+`)

func codegenproto(w io.Writer, proto protocol) error {
	if _, err := fmt.Fprintf(w, "////////////////////////////////////////////////////////////////////////////////\n// #region Protocol %s\n\n", proto.Name); err != nil {
		return fmt.Errorf("writing protocol %s begin region: %v", proto.Name, err)
	}

	for _, intf := range proto.Interfaces {
		if _, err := fmt.Fprintf(w, "// ----------------------------------------------------------------------------\n// #region Interface %s.%s\n\n", proto.Name, intf.Name); err != nil {
			return fmt.Errorf("writing protocol %s begin region: %v", intf.Name, err)
		}

		// Generate enums
		for _, enum := range intf.Enums {
			// Bitfields are uint; other enums are just int.
			typ := "int"
			if enum.Bitfield {
				typ = "uint"
			}

			enumname := namegen(intf.Name, enum.Name)

			// Make doc comment.
			if err := docgen(w, enumname, enum.Description, " represents ", ""); err != nil {
				return fmt.Errorf("writing enum %s doc comment: %v", enumname, err)
			}

			// Make type declaration.
			if _, err := fmt.Fprintf(w, "type %s %s\n", enumname, typ); err != nil {
				return fmt.Errorf("writing enum %s type declaration: %v", enumname, err)
			}

			// Make entry constants.
			fmt.Fprintf(w, "const (\n")
			for _, entry := range enum.Entries {
				entryname := namegen(intf.Name, enum.Name, entry.Name)

				if err := docgen(w, entryname, description{Summary: entry.Summary}, " corresponds to ", "\t"); err != nil {
					return fmt.Errorf("writing enum entry %s doc comment: %v", entryname, err)
				}

				if _, err := fmt.Fprintf(w, "\t%s %s = %s\n\n", entryname, enumname, entry.Value); err != nil {
					return fmt.Errorf("writing enum entry %s declaration: %v", entryname, err)
				}
			}
			fmt.Fprint(w, ")\n\n")
		}

		// Generate request structs.
		for opcode, request := range intf.Requests {
			structname := namegen(intf.Name, request.Name, "request")

			// Make doc comment.
			if err := docgen(w, structname, request.Description, " requests to ", ""); err != nil {
				return fmt.Errorf("writing request %s doc comment: %v", structname, err)
			}

			// Open struct declaration.
			if _, err := fmt.Fprintf(w, "type %s struct {\n", structname); err != nil {
				return fmt.Errorf("writing request %s struct open: %v", structname, err)
			}

			// Write arguments.
			for _, arg := range request.Args {
				if err := arggen(w, arg); err != nil {
					return fmt.Errorf("writing request %s struct: %v", structname, err)
				}
			}

			// Close struct declaration.
			if _, err := fmt.Fprint(w, "}\n\n"); err != nil {
				return fmt.Errorf("writing request %s struct close: %v", structname, err)
			}

			// Implement Opcode function.
			if _, err := fmt.Fprintf(w,
				"// Opcode returns the request opcode for %s.%s in %s\nfunc (%s) Opcode() uint16 { return %d }\n\n",
				intf.Name, request.Name, proto.Name, structname, opcode); err != nil {
				return fmt.Errorf("writing request %s Opcode implementation: %v", structname, err)
			}

			// Ensure implementation of Message
			if _, err := fmt.Fprintf(w, "// Ensure %s implements Message.\nvar _ Message = %s{}\n\n", structname, structname); err != nil {
				return fmt.Errorf("writing request %s Message interface check: %v", structname, err)
			}

			// Implement Emit function.
			if _, err := fmt.Fprintf(w,
				"// Emit emits the message to the emitter.\nfunc (r *%s) Emit(e *RequestEmitter) error {\n", structname); err != nil {
				return fmt.Errorf("writing request %s Emit function header: %v", structname, err)
			}

			// Write argument emitters.
			for _, arg := range request.Args {
				if err := argemitgen(w, arg); err != nil {
					return err
				}
			}

			if _, err := fmt.Fprintf(w, "\treturn nil\n}\n"); err != nil {
				return fmt.Errorf("writing request %s Emit function footer: %v", structname, err)
			}

			// Ensure implementation of Request
			if _, err := fmt.Fprintf(w, "// Ensure %s implements Request.\nvar _ Request = &%s{}\n\n", structname, structname); err != nil {
				return fmt.Errorf("writing request %s Request interface check: %v", structname, err)
			}
		}

		// Generate event structs.
		for opcode, event := range intf.Events {
			structname := namegen(intf.Name, event.Name, "event")

			// Make doc comment.
			if err := docgen(w, structname, event.Description, " signals when ", ""); err != nil {
				return fmt.Errorf("writing event %s doc comment: %v", structname, err)
			}

			// Open struct declaration.
			if _, err := fmt.Fprintf(w, "type %s struct {\n", structname); err != nil {
				return fmt.Errorf("writing event %s struct open: %v", structname, err)
			}

			// Write arguments.
			for _, arg := range event.Args {
				if err := arggen(w, arg); err != nil {
					return fmt.Errorf("writing event %s struct: %v", structname, err)
				}
			}

			// Close struct declaration.
			if _, err := fmt.Fprint(w, "}\n\n"); err != nil {
				return fmt.Errorf("writing event %s struct close: %v", structname, err)
			}

			// Implement Opcode function.
			if _, err := fmt.Fprintf(w,
				"// Opcode returns the event opcode for %s.%s in %s\nfunc (%s) Opcode() uint16 { return %d }\n\n",
				intf.Name, event.Name, proto.Name, structname, opcode); err != nil {
				return fmt.Errorf("writing event %s Opcode implementation: %v", structname, err)
			}

			// Ensure implementation of Message
			if _, err := fmt.Fprintf(w, "// Ensure %s implements Message.\nvar _ Message = %s{}\n\n", structname, structname); err != nil {
				return fmt.Errorf("writing event %s Message interface check: %v", structname, err)
			}

			// Implement Scan function.
			if _, err := fmt.Fprintf(w,
				"// Scan scans the event from the socket.\nfunc (e *%s) Scan(s *EventScanner) error {\n", structname); err != nil {
				return fmt.Errorf("writing event %s Scan function header: %v", structname, err)
			}

			// Write argument scanners.
			for _, arg := range event.Args {
				if err := argscangen(w, arg); err != nil {
					return err
				}
			}

			if _, err := fmt.Fprintf(w, "\treturn nil\n}\n"); err != nil {
				return fmt.Errorf("writing event %s Scan function footer: %v", structname, err)
			}

			// Ensure implementation of Event
			if _, err := fmt.Fprintf(w, "// Ensure %s implements Event.\nvar _ Event = &%s{}\n\n", structname, structname); err != nil {
				return fmt.Errorf("writing event %s Event interface check: %v", structname, err)
			}
		}

		// Generate proxy.
		structname := namegen(intf.Name)

		// Make doc comment.
		if err := docgen(w, structname, intf.Description, " ", ""); err != nil {
			return fmt.Errorf("writing proxy %s doc comment: %v", structname, err)
		}

		// Proxy struct declaration.
		if _, err := fmt.Fprintf(w, "type %s struct {\n\tid ObjectID\n}\n\n", structname); err != nil {
			return fmt.Errorf("writing proxy %s struct: %v", structname, err)
		}

		if _, err := fmt.Fprintf(w, "// #endregion Interface %s.%s\n\n", proto.Name, intf.Name); err != nil {
			return fmt.Errorf("writing protocol %s end region: %v", intf.Name, err)
		}
	}

	if _, err := fmt.Fprintf(w, "////////////////////////////////////////////////////////////////////////////////\n// #endregion Protocol %s\n\n", proto.Name); err != nil {
		return fmt.Errorf("writing protocol %s end region: %v", proto.Name, err)
	}

	return nil
}

func arggen(w io.Writer, arg arg) error {
	argname := namegen(arg.Name)

	// Make doc comment.
	if err := docgen(w, argname, description{Summary: arg.Summary}, " contains ", "\t"); err != nil {
		return fmt.Errorf("writing argument %s doc comment: %v", argname, err)
	}

	typ := ""
	switch arg.Type {
	case "int":
		typ = "int32"
	case "uint":
		typ = "uint32"
	case "fixed":
		typ = "Fixed"
	case "object", "new_id":
		typ = "ObjectID"
	case "string":
		typ = "string"
	case "array":
		typ = "[]byte"
	case "fd":
		typ = "FD"
	default:
		return fmt.Errorf("argument %s: unknown argument type %q", argname, arg.Type)
	}

	// Write actual arg.
	if _, err := fmt.Fprintf(w, "\t%s %s\n\n", argname, typ); err != nil {
		return fmt.Errorf("writing argument %s: %v", argname, err)
	}

	return nil
}

func argscangen(w io.Writer, arg arg) error {
	typ, err := argtypfn(arg)
	if err != nil {
		return err
	}

	argname := namegen(arg.Name)

	if _, err := fmt.Fprintf(w, "\tif v, err := s.%s(); err != nil {\n\t\treturn err\n\t} else {\n\t\te.%s = v\n\t}\n", typ, argname); err != nil {
		return fmt.Errorf("writing argument scanner %s: %v", argname, err)
	}

	return nil
}

func argemitgen(w io.Writer, arg arg) error {
	typ, err := argtypfn(arg)
	if err != nil {
		return err
	}

	argname := namegen(arg.Name)

	if _, err := fmt.Fprintf(w, "\tif err := e.Put%s(r.%s); err != nil {\n\t\treturn err\n\t}\n", typ, argname); err != nil {
		return fmt.Errorf("writing argument emitter %s: %v", argname, err)
	}

	return nil
}

func argtypfn(arg arg) (string, error) {
	switch arg.Type {
	case "int":
		return "Int", nil
	case "uint":
		return "Uint", nil
	case "fixed":
		return "Fixed", nil
	case "object", "new_id":
		return "ObjectID", nil
	case "string":
		return "String", nil
	case "array":
		return "Array", nil
	case "fd":
		return "FD", nil
	default:
		return "", fmt.Errorf("argument %s: unknown argument type %q", namegen(arg.Name), arg.Type)
	}
}

func docgen(w io.Writer, name string, desc description, filler string, prefix string) error {
	// Make doc comment.
	if desc.Summary != "" {
		// Summary
		summary := strings.TrimSpace(spacesRE.ReplaceAllString(desc.Summary, " "))
		if _, err := fmt.Fprintf(w, "%s// %s%s%s\n", prefix, name, filler, summary); err != nil {
			return err
		}

		// Full documentation
		text := strings.TrimSpace(desc.Text)
		if text != "" {
			if _, err := fmt.Fprintf(w, "%s//\n", prefix); err != nil {
				return err
			}
			for _, line := range strings.Split(text, "\n") {
				if _, err := fmt.Fprintf(w, "%s// %s\n", prefix, strings.TrimSpace(line)); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func namegen(names ...string) string {
	b := strings.Builder{}

	for _, name := range names {
		for _, part := range strings.Split(name, "_") {
			if part == "" {
				continue
			}

			switch part {
			case "id", "fd":
				b.WriteString(strings.ToUpper(part))

			default:
				if part[0] >= 'a' && part[0] <= 'z' {
					b.WriteByte(part[0] & 0b11011111)
					b.WriteString(part[1:])
				} else {
					b.WriteString(part)
				}
			}
		}
	}

	return b.String()
}
