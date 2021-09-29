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

// This package provides a minimal implementation of language.Language for
// rules_sass.
package gazelle

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

var _ = fmt.Printf

const extName = "js"

type jslang struct{}

// NewLanguage returns an instace of the Gazelle plugin for rules_sass.
func NewLanguage() language.Language {
	return &jslang{}
}

// Kinds returns a map of maps rule names (kinds) and information on how to
// match and merge attributes that may be found in rules of those kinds. All
// kinds of rules generated for this language may be found here.
func (s *jslang) Kinds() map[string]rule.KindInfo {
	return map[string]rule.KindInfo{
		"js_library": {
			MatchAny: false,
			NonEmptyAttrs: map[string]bool{
				"srcs": true,
			},
			MergeableAttrs: map[string]bool{
				"srcs": true,
			},
			ResolveAttrs: map[string]bool{"deps": true},
		},
		"jest_test": {
			MatchAny: false,
			NonEmptyAttrs: map[string]bool{
				"srcs": true,
			},
			MergeableAttrs: map[string]bool{
				"srcs": true,
			},
			ResolveAttrs: map[string]bool{
				"deps":   true,
				"config": true,
			},
		},
		"js_import": {
			MatchAny: false,
			ResolveAttrs: map[string]bool{
				"deps":   true,
				"config": true,
			},
			NonEmptyAttrs: map[string]bool{
				"srcs": true,
			},
			MergeableAttrs: map[string]bool{
				"srcs": true,
			},
		},
		"ts_project": {
			MatchAny: false,
			NonEmptyAttrs: map[string]bool{
				"srcs": true,
			},
			MergeableAttrs: map[string]bool{
				"srcs": true,
			},
			ResolveAttrs: map[string]bool{"deps": true},
		},
	}
}

// Loads returns .bzl files and symbols they define. Every rule generated by
// GenerateRules, now or in the past, should be loadable from one of these
// files.
func (s *jslang) Loads() []rule.LoadInfo {
	return []rule.LoadInfo{
		{
			Name:    "@benchsci_test_tools_js//:defs.bzl",
			Symbols: []string{"js_library", "ts_project", "jest_test", "js_import"},
		},
	}
}

func trimExt(filename string) string {
        extension := filepath.Ext(filename)
	return "_" + strings.Replace(extension, ".", "", -1)
}

func containsSuffix(suffixes []string, x string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(x, suffix) {
			return true
		}
	}
	return false
}

// GenerateRules extracts build metadata from source files in a directory.
// GenerateRules is called in each directory where an update is requested
// in depth-first post-order.
//
// args contains the arguments for GenerateRules. This is passed as a
// struct to avoid breaking implementations in the future when new
// fields are added.
//
// empty is a list of empty rules that may be deleted after merge.
//
// gen is a list of generated rules that may be updated or added.
//
// Any non-fatal errors this function encounters should be logged using
// log.Print.
func (s *jslang) GenerateRules(args language.GenerateArgs) language.GenerateResult {
	c := args.Config
	js := GetJsConfig(c)
	// base is the last part of the path for this element. For example:
	// "hello_world" => "hello_world"
	// log.Println(args.OtherGen)
	base := path.Base(args.Rel)
	if base == "." {
		//args.Rel will return an empty string if you're in the root of the repo.
		//This will then be translated into "." by path.Base which is not a valid
		//name for a target. Therefore we will use the name of "root" just to have
		//something that is valid. If the user doesn't want the target to be named
		// `//:root`, then they can rename it and on the next generation it will
		// persist the user supplied name.
		base = "root"
	}

	rules := []*rule.Rule{}
	imports := []interface{}{}
	empty := []*rule.Rule{}
	var jsFiles []string
	var jsImportFiles []string

	// var normalFiles []string
	for _, f := range append(args.RegularFiles, args.GenFiles...) {

		base = (path.Base(f))
		prefix := trimExt(base)
		base = strings.TrimSuffix(base, filepath.Ext(base))
		if containsSuffix(js.JsImportExtenstions, f) {
			rule := rule.NewRule("js_import", base + prefix)
			rule.SetAttr("srcs", []string{f})
			// TODO: Ideally we would not just apply public visibility
			rule.SetAttr("visibility", []string{"//visibility:public"})
			rules = append(rules, rule)
			slice := []string{}
			imports = append(imports, slice)
		}
		// Only generate js entries for known js files (.vue/.js) - can probably be extended
		if (!strings.HasSuffix(f, ".vue") && !strings.HasSuffix(f, ".js") && !strings.HasSuffix(f, ".jsx") && !strings.HasSuffix(f, ".tsx") && !strings.HasSuffix(f, ".ts")) ||
			strings.HasSuffix(f, "k6.js") ||
			strings.HasSuffix(f, "e2e.test.js") ||
			(!js.GenerateTests && strings.HasSuffix(f, ".test.js")) {
			jsImportFiles = append(jsImportFiles, f)
			continue
		}

		fileInfo := jsFileinfo(args.Dir, f)
		imports = append(imports, fileInfo.Imports)
		jsFiles = append(jsFiles, f)


		if strings.HasSuffix(f, ".test.js") {
			rule := rule.NewRule("jest_test", base)
			rule.SetAttr("srcs", []string{f})
			// rule.SetAttr("entry_point", "@"+js.NpmWorkspaceName+"//:node_modules/jest-cli/bin/jest.js")
			// This is currently not possible. See: https://github.com/bazelbuild/bazel-gazelle/issues/511
			// rule.SetAttr("env", map[string]string{"NODE_ENV": "test"})
			// rule.SetAttr("jest", "@"+js.NpmWorkspaceName+"//jest/bin:jest")
			// rule.SetAttr("max_workers", "1")
			rules = append(rules, rule)
		} else if strings.HasSuffix(f, "test.ts") {
			rule := rule.NewRule("jest_test", base)
			rule.SetAttr("srcs", []string{f})
			// TODO: Ideally we would not just apply public visibility
			//rule.SetAttr("visibility", []string{"//visibility:public"})
			rules = append(rules, rule)
		} else if strings.HasSuffix(f, ".ts") {
			rule := rule.NewRule("ts_project", base)
			rule.SetAttr("srcs", []string{f})
			// TODO: Ideally we would not just apply public visibility
			rule.SetAttr("visibility", []string{"//visibility:public"})
			rules = append(rules, rule)
		} else if strings.HasSuffix(f, ".tsx") {
			rule := rule.NewRule("ts_project", base)
			rule.SetAttr("srcs", []string{f})
			// TODO: Ideally we would not just apply public visibility
			rule.SetAttr("visibility", []string{"//visibility:public"})
			rules = append(rules, rule)
		} else {
			rule := rule.NewRule(js.JsLibrary.String(), base)
			rule.SetAttr("srcs", []string{f})
			// TODO: Ideally we would not just apply public visibility
			rule.SetAttr("visibility", []string{"//visibility:public"})
			rules = append(rules, rule)
		}
	}

	empty = append(empty, generateEmpty(args.File, jsFiles, map[string]bool{js.JsLibrary.String(): true, "jest_test": true, "ts_library": true})...)

	if len(js.JsImportExtenstions) > 0 {
		empty = append(empty, generateEmpty(args.File, jsImportFiles, map[string]bool{"js_import": true})...)
	}

	return language.GenerateResult{
		Gen:     rules,
		Imports: imports,
		// Empty is a list of rules that cannot be built with the files found in the
		// directory GenerateRules was asked to process. These will be merged with
		// existing rules. If ther merged rules are empty, they will be deleted.
		// In order to keep the BUILD file clean, if no file is included in the
		// default rule for this directory, then remove it.
		Empty: empty,
	}
}

// generateEmpty generates a list of jest_test, js_library and js_import rules that may be
// deleted. This is generated from these existing rules with srcs lists that don't match any
// static or generated files.
func generateEmpty(f *rule.File, files []string, knownRuleKinds map[string]bool) []*rule.Rule {
	if f == nil {
		return nil
	}
	knownFiles := make(map[string]bool)
	for _, f := range files {
		knownFiles[f] = true
	}
	var empty []*rule.Rule
outer:
	for _, r := range f.Rules {
		if !knownRuleKinds[r.Kind()] {
			continue
		}
		srcs := r.AttrStrings("srcs")
		if len(srcs) == 0 && r.Attr("srcs") != nil {
			// srcs is not a string list; leave it alone
			continue
		}
		for _, src := range r.AttrStrings("srcs") {
			if knownFiles[src] {
				continue outer
			}
		}
		empty = append(empty, rule.NewRule(r.Kind(), r.Name()))
	}
	return empty
}

// Fix repairs deprecated usage of language-specific rules in f. This is
// called before the file is indexed. Unless c.ShouldFix is true, fixes
// that delete or rename rules should not be performed.
func (s *jslang) Fix(c *config.Config, f *rule.File) {
}
