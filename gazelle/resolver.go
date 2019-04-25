/* Copyright 2019 The Bazel Authors. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

   http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package gazelle

import (
	"errors"
	"fmt"
	"log"
	"path"
	"sort"
	"strings"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/label"
	"github.com/bazelbuild/bazel-gazelle/repo"
	"github.com/bazelbuild/bazel-gazelle/resolve"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

var _ = fmt.Printf

var (
	skipImportError = errors.New("std import")
	notFoundError   = errors.New("not found")
)

// Name returns the name of the language. This should be a prefix of the
// kinds of rules generated by the language, e.g., "go" for the Go extension
// since it generates "go_library" rules.
func (s *jslang) Name() string {
	return "js"
}

// Imports returns a list of ImportSpecs that can be used to import the rule
// r. This is used to populate RuleIndex.
//
// If nil is returned, the rule will not be indexed. If any non-nil slice is
// returned, including an empty slice, the rule will be indexed.
func (s *jslang) Imports(c *config.Config, r *rule.Rule, f *rule.File) []resolve.ImportSpec {
	rel := f.Pkg
	srcs := r.AttrStrings("srcs")
	imports := make([]resolve.ImportSpec, len(srcs))
	for i, src := range srcs {
		withoutSuffix := strings.TrimSuffix(src, path.Ext(src))
		imports[i] = resolve.ImportSpec{
			Lang: "js",
			Imp:  strings.ToLower(path.Join(rel, withoutSuffix)),
		}
	}
	return imports
}

// Embeds returns a list of labels of rules that the given rule embeds. If
// a rule is embedded by another importable rule of the same language, only
// the embedding rule will be indexed. The embedding rule will inherit
// the imports of the embedded rule.
func (s *jslang) Embeds(r *rule.Rule, from label.Label) []label.Label {
	// Sass doesn't have a concept of embedding as far as I know.
	return nil
}

// Resolve translates imported libraries for a given rule into Bazel
// dependencies. A list of imported libraries is typically stored in a
// private attribute of the rule when it's generated (this interface doesn't
// dictate how that is stored or represented). Resolve generates a "deps"
// attribute (or the appropriate language-specific equivalent) for each
// import according to language-specific rules and heuristics.
func (s *jslang) Resolve(c *config.Config, ix *resolve.RuleIndex, rc *repo.RemoteCache, r *rule.Rule, importsRaw interface{}, from label.Label) {
	imports := importsRaw.([]string)
	r.DelAttr("deps")
	depSet := make(map[string]bool)
	for _, imp := range imports {
		imp = normaliseImports(imp, ix, from)
		l, err := resolveWithIndex(ix, imp, from)
		if err == skipImportError {
			continue
		} else if err == notFoundError {
			// npm dependencies are currently not part of the index and would return this error
			// TODO: Find some way to customise the name of the npm repository. Or maybe this can be fixed somehow by indexing external deps?
			if isNpmDependency(imp) {
				s := strings.Split(imp, "/")
				imp = s[0]
				if strings.HasPrefix(imp, "@") {
					imp += "/" + s[1]
				}
				depSet["@npm//"+imp] = true
			} else {
				log.Printf("Import %v not found.\n", imp)
			}
		} else if err != nil {
			log.Print(err)
		} else {
			l = l.Rel(from.Repo, from.Pkg)
			depSet[l.String()] = true
		}
	}
	if len(depSet) > 0 {
		deps := make([]string, 0, len(depSet))
		for dep := range depSet {
			deps = append(deps, dep)
		}
		sort.Strings(deps)
		r.SetAttr("deps", deps)
	}
	if r.Kind() == "jest_node_test" {
		l, err := findJsConfig("jest", ix, from)
		if err != nil {
			log.Printf("Jest config %v", err)
		} else {
			l = l.Rel(from.Repo, from.Pkg)
			r.SetAttr("config", l.String())
		}
	}
}

// Note: Ideall this was not necessary and the jest rule would not need a jest config defined in the workspace
func findJsConfig(configName string, ix *resolve.RuleIndex, from label.Label) (label.Label, error) {
	pkgDir := from.Pkg
	for pkgDir != ".." {
		imp := path.Join(pkgDir, configName+".config")
		label, err := resolveWithIndex(ix, imp, from)
		if err == nil {
			return label, err
		}
		pkgDir = path.Join(pkgDir, "..")
	}
	return label.NoLabel, notFoundError
}

func findVueConfig(ix *resolve.RuleIndex, from label.Label) (label.Label, error) {
	pkgDir := from.Pkg
	for pkgDir != ".." {
		imp := path.Join(pkgDir, "vue.config")
		label, err := resolveWithIndex(ix, imp, from)
		if err == nil {
			return label, err
		}
		pkgDir = path.Join(pkgDir, "..")
	}
	return label.NoLabel, notFoundError
}

// Taken from https://nodejs.org/api/modules.html#modules_all_together and extended by some common aliases to make sure
// we do not accidentally treat them as an npm package
func isNpmDependency(imp string) bool {
	isSourceDep := strings.HasPrefix(imp, "./") || strings.HasPrefix(imp, "/") || strings.HasPrefix(imp, "../") || strings.HasPrefix(imp, "~/") || strings.HasPrefix(imp, "@/") || strings.HasPrefix(imp, "~~/")
	return !isSourceDep
}

// normaliseImports ensures that relative imports or alias imports can all resolve to the same file
func normaliseImports(imp string, ix *resolve.RuleIndex, from label.Label) string {
	pkgDir := from.Pkg
	// TODO: Right now we assume @/ and ~~ to simply be an alias for imports from the root, but that might not be true.
	// Also need to support ~ aliases which is even more tricky
	if strings.HasPrefix(imp, "@/") {
		return imp[2:]
	}

	if strings.HasPrefix(imp, "~~/") {
		return imp[3:]
	}

	if strings.HasPrefix(imp, "~/") {
		// TODO: Figure out if we want to ignore any config files found at root
		l, err := findJsConfig("nuxt", ix, from)
		configFound := "nuxt"
		if err != nil {
			l, err = findJsConfig("vue", ix, from)
			configFound = "vue"
		}

		if err == nil {
			basePath := path.Dir(l.Rel(from.Repo, from.Pkg).String())

			// TODO: Do not hardcode the basePath for the vueConfig but actually check if a src directory is present
			// at basePath
			if configFound == "vue" {
				basePath = path.Join(basePath, "src")
			}
			return path.Join(basePath, imp)
		}
	}

	if strings.HasPrefix(imp, "../") {
		return path.Join(pkgDir, imp)
	}

	if strings.HasPrefix(imp, "./") {
		return path.Join(pkgDir, imp)
	}

	return imp
}

func resolveWithIndex(ix *resolve.RuleIndex, imp string, from label.Label) (label.Label, error) {
	res := resolve.ImportSpec{
		Lang: "js",
		Imp:  imp,
	}
	matches := ix.FindRulesByImport(res, "js")
	if len(matches) == 0 {
		return label.NoLabel, notFoundError
	}
	if len(matches) > 1 {
		return label.NoLabel, fmt.Errorf("multiple rules (%s and %s) may be imported with %q from %s", matches[0].Label, matches[1].Label, imp, from)
	}
	if matches[0].IsSelfImport(from) {
		return label.NoLabel, skipImportError
	}
	return matches[0].Label, nil
}
