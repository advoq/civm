package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/emersonbusson/civm/internal/specs"
)

func runVersionPins(args []string) int {
	fs := flag.NewFlagSet("version-pins", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOut := fs.Bool("json", false, "saida JSON em vez de tabela")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, "erro nos args de version-pins:", err)
		return exitUsage
	}
	spec := specs.Ubuntu2404()
	if *jsonOut {
		return renderSpecJSON(spec, os.Stdout)
	}
	fmt.Print(spec.Render())
	return 0
}

func renderSpecJSON(s specs.RunnerImageSpec, w io.Writer) int {
	fmt.Fprintln(w, "{")
	fmt.Fprintf(w, "  \"os_distro\": %q,\n", s.OSDistro)
	fmt.Fprintf(w, "  \"os_version\": %q,\n", s.OSVersion)
	fmt.Fprintf(w, "  \"kernel\": %q,\n", s.Kernel)
	fmt.Fprintf(w, "  \"image_version\": %q,\n", s.ImageVersion)
	fmt.Fprintf(w, "  \"upstream_url\": %q,\n", s.UpstreamURL)
	fmt.Fprintln(w, "  \"tools\": [")
	for i, t := range s.Tools {
		fmt.Fprintf(w, "    {\"name\": %q, \"preferred\": %q, \"versions\": [", t.Name, t.Preferred())
		for j, v := range t.Versions {
			if j > 0 {
				fmt.Fprint(w, ", ")
			}
			fmt.Fprintf(w, "%q", v)
		}
		fmt.Fprintf(w, "], \"source\": %q}", t.Source)
		if i < len(s.Tools)-1 {
			fmt.Fprintln(w, ",")
		} else {
			fmt.Fprintln(w)
		}
	}
	fmt.Fprintln(w, "  ]")
	fmt.Fprintln(w, "}")
	return 0
}
