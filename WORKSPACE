workspace(name = "com_github_google_wire")

load("@bazel_tools//tools/build_defs/repo:http.bzl", "http_archive")

http_archive(
    name = "rules_pkg",
    sha256 = "6b5969a7acd7b60c02f816773b06fcf32fbe8ba0c7919ccdc2df4f8fb923804a",
    urls = [
        "https://mirror.bazel.build/github.com/bazelbuild/rules_pkg/releases/download/0.3.0/rules_pkg-0.3.0.tar.gz",
        "https://github.com/bazelbuild/rules_pkg/releases/download/0.3.0/rules_pkg-0.3.0.tar.gz",
    ],
)

load("@rules_pkg//:deps.bzl", "rules_pkg_dependencies")

rules_pkg_dependencies()

http_archive(
    name = "io_bazel_rules_go",
    sha256 = "d1ffd055969c8f8d431e2d439813e42326961d0942bdf734d2c95dc30c369566",
    urls = [
        "https://mirror.bazel.build/github.com/bazelbuild/rules_go/releases/download/v0.24.5/rules_go-v0.24.5.tar.gz",
        "https://github.com/bazelbuild/rules_go/releases/download/v0.24.5/rules_go-v0.24.5.tar.gz",
    ],
)

http_archive(
    name = "bazel_gazelle",
    sha256 = "b85f48fa105c4403326e9525ad2b2cc437babaa6e15a3fc0b1dbab0ab064bc7c",
    urls = [
        "https://mirror.bazel.build/github.com/bazelbuild/bazel-gazelle/releases/download/v0.22.2/bazel-gazelle-v0.22.2.tar.gz",
        "https://github.com/bazelbuild/bazel-gazelle/releases/download/v0.22.2/bazel-gazelle-v0.22.2.tar.gz",
    ],
)

load("@io_bazel_rules_go//go:deps.bzl", "go_register_toolchains", "go_rules_dependencies")

go_rules_dependencies()

go_register_toolchains()

load("@bazel_gazelle//:deps.bzl", "gazelle_dependencies", "go_repository")

go_repository(
    name = "com_github_google_go_cmp",
    importpath = "github.com/google/go-cmp",
    sum = "h1:+dTQ8DZQJz0Mb/HjFlkptS1FeQ4cWSnN941F8aEG4SQ=",
    version = "v0.2.0",
)

go_repository(
    name = "com_github_google_subcommands",
    importpath = "github.com/google/subcommands",
    sum = "h1:/eqq+otEXm5vhfBrbREPCSVQbvofip6kIz+mX5TUH7k=",
    version = "v1.0.1",
)

go_repository(
    name = "com_github_pmezard_go_difflib",
    importpath = "github.com/pmezard/go-difflib",
    sum = "h1:4DBwDE0NGyQoBHbLQYPwSUPoCMWR5BEzIk/f1lZbAQM=",
    version = "v1.0.0",
)

go_repository(
    name = "org_golang_x_crypto",
    importpath = "golang.org/x/crypto",
    sum = "h1:ObdrDkeb4kJdCP557AjRjq69pTHfNouLtWZG7j9rPN8=",
    version = "v0.0.0-20191011191535-87dc89f01550",
)

go_repository(
    name = "org_golang_x_net",
    importpath = "golang.org/x/net",
    sum = "h1:R/3boaszxrf1GEUWTVDzSKVwLmSJpwZ1yqXm8j0v2QI=",
    version = "v0.0.0-20190620200207-3b0461eec859",
)

go_repository(
    name = "org_golang_x_sys",
    importpath = "golang.org/x/sys",
    sum = "h1:+R4KGOnez64A81RvjARKc4UT5/tI9ujCIVX+P5KiHuI=",
    version = "v0.0.0-20190412213103-97732733099d",
)

go_repository(
    name = "org_golang_x_text",
    importpath = "golang.org/x/text",
    sum = "h1:g61tztE5qeGQ89tm6NTjjM9VPIm088od1l6aSorWRWg=",
    version = "v0.3.0",
)

go_repository(
    name = "org_golang_x_tools",
    build_extra_args = [
        "-exclude=cmd/bundle/testdata",
        "-exclude=cmd/callgraph/testdata",
        "-exclude=cmd/cover/testdata",
        "-exclude=cmd/fiximports/testdata",
        "-exclude=cmd/goyacc/testdata",
        "-exclude=cmd/guru/testdata",
        "-exclude=cmd/splitdwarf/internal/macho/testdata",
        "-exclude=cmd/stringer/testdata",
        "-exclude=go/analysis/passes/asmdecl/testdata",
        "-exclude=go/analysis/passes/assign/testdata",
        "-exclude=go/analysis/passes/atomic/testdata",
        "-exclude=go/analysis/passes/atomicalign/testdata",
        "-exclude=go/analysis/passes/bools/testdata",
        "-exclude=go/analysis/passes/buildssa/testdata",
        "-exclude=go/analysis/passes/buildtag/testdata",
        "-exclude=go/analysis/passes/cgocall/testdata",
        "-exclude=go/analysis/passes/composite/testdata",
        "-exclude=go/analysis/passes/copylock/testdata",
        "-exclude=go/analysis/passes/ctrlflow/testdata",
        "-exclude=go/analysis/passes/deepequalerrors/testdata",
        "-exclude=go/analysis/passes/errorsas/testdata",
        "-exclude=go/analysis/passes/findcall/testdata",
        "-exclude=go/analysis/passes/framepointer/testdata",
        "-exclude=go/analysis/passes/httpresponse/testdata",
        "-exclude=go/analysis/passes/ifaceassert/testdata",
        "-exclude=go/analysis/passes/loopclosure/testdata",
        "-exclude=go/analysis/passes/lostcancel/testdata",
        "-exclude=go/analysis/passes/nilfunc/testdata",
        "-exclude=go/analysis/passes/nilness/testdata",
        "-exclude=go/analysis/passes/pkgfact/testdata",
        "-exclude=go/analysis/passes/printf/testdata",
        "-exclude=go/analysis/passes/shadow/testdata",
        "-exclude=go/analysis/passes/shift/testdata",
        "-exclude=go/analysis/passes/sortslice/testdata",
        "-exclude=go/analysis/passes/stdmethods/testdata",
        "-exclude=go/analysis/passes/stringintconv/testdata",
        "-exclude=go/analysis/passes/structtag/testdata",
        "-exclude=go/analysis/passes/testinggoroutine/testdata",
        "-exclude=go/analysis/passes/tests/testdata",
        "-exclude=go/analysis/passes/unmarshal/testdata",
        "-exclude=go/analysis/passes/unreachable/testdata",
        "-exclude=go/analysis/passes/unsafeptr/testdata",
        "-exclude=go/analysis/passes/unusedresult/testdata",
        "-exclude=go/callgraph/cha/testdata",
        "-exclude=go/callgraph/rta/testdata",
        "-exclude=go/expect/testdata",
        "-exclude=go/gccgoexportdata/testdata",
        "-exclude=go/gcexportdata/testdata",
        "-exclude=go/internal/gccgoimporter/testdata",
        "-exclude=go/internal/gcimporter/testdata",
        "-exclude=go/loader/testdata",
        "-exclude=go/packages/packagestest/testdata",
        "-exclude=go/pointer/testdata",
        "-exclude=go/ssa/interp/testdata",
        "-exclude=go/ssa/ssautil/testdata",
        "-exclude=go/ssa/testdata",
        "-exclude=internal/apidiff/testdata",
        "-exclude=internal/imports/testdata",
        "-exclude=internal/lsp/analysis/fillreturns/testdata",
        "-exclude=internal/lsp/analysis/fillstruct/testdata",
        "-exclude=internal/lsp/analysis/nonewvars/testdata",
        "-exclude=internal/lsp/analysis/noresultvalues/testdata",
        "-exclude=internal/lsp/analysis/simplifycompositelit/testdata",
        "-exclude=internal/lsp/analysis/simplifyrange/testdata",
        "-exclude=internal/lsp/analysis/simplifyslice/testdata",
        "-exclude=internal/lsp/analysis/undeclaredname/testdata",
        "-exclude=internal/lsp/analysis/unusedparams/testdata",
        "-exclude=internal/lsp/mod/testdata",
        "-exclude=internal/lsp/testdata",
        "-exclude=present/testdata",
        "-exclude=refactor/eg/testdata",
    ],
    importpath = "golang.org/x/tools",
    sum = "h1:aZzprAO9/8oim3qStq3wc1Xuxx4QmAGriC4VU4ojemQ=",
    version = "v0.0.0-20191119224855-298f0cb1881e",
)

go_repository(
    name = "org_golang_x_mod",
    importpath = "golang.org/x/mod",
    sum = "h1:8pl+sMODzuvGJkmj2W4kZihvVb5mKm8pB/X44PIQHv8=",
    version = "v0.4.0",
)

go_repository(
    name = "org_golang_x_sync",
    importpath = "golang.org/x/sync",
    sum = "h1:8gQV6CLnAEikrhgkHFbMAEhagSSnXWGV915qUMm9mrU=",
    version = "v0.0.0-20190423024810-112230192c58",
)

go_repository(
    name = "org_golang_x_xerrors",
    importpath = "golang.org/x/xerrors",
    sum = "h1:/atklqdjdhuosWIl6AIbOeHJjicWYPqR9bpxqxYG2pA=",
    version = "v0.0.0-20191011141410-1b5146add898",
)

gazelle_dependencies()
