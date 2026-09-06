# The binary is prebuilt and stripped, so there is nothing for rpmbuild to
# extract debug symbols from. Without this it fails on an empty debugsource
# file list rather than simply producing no debug package.
%global debug_package %{nil}

Name:           reclaim
Version:        %{_version}
Release:        1%{?dist}
Summary:        Reclaim disk space without losing anything you cannot get back

License:        MIT
URL:            https://github.com/mralaminahamed/reclaim
Source0:        %{name}-%{version}.tar.gz

# The binary is built before rpmbuild runs and shipped in the source tarball,
# so this needs no Go toolchain and no network at package time.
BuildArch:      x86_64
AutoReqProv:    no

%description
reclaim measures and removes regenerable data - package caches, build
artifacts, browser caches - ordered by how expensive each one is to get back.
It refuses to cross a reversibility ceiling, skips caches belonging to
applications that are currently running, and deletes nothing without --apply.

%prep
%setup -q -c

%install
install -Dm0755 reclaim %{buildroot}%{_bindir}/reclaim
install -d %{buildroot}%{_datadir}/bash-completion/completions
install -d %{buildroot}%{_datadir}/zsh/site-functions
install -d %{buildroot}%{_datadir}/fish/vendor_completions.d
./reclaim completion bash > %{buildroot}%{_datadir}/bash-completion/completions/reclaim
./reclaim completion zsh  > %{buildroot}%{_datadir}/zsh/site-functions/_reclaim
./reclaim completion fish > %{buildroot}%{_datadir}/fish/vendor_completions.d/reclaim.fish

%files
%{_bindir}/reclaim
%{_datadir}/bash-completion/completions/reclaim
%{_datadir}/zsh/site-functions/_reclaim
%{_datadir}/fish/vendor_completions.d/reclaim.fish

%changelog
* Sun Sep 06 2026 Al Amin Ahamed <n.mukto@codexpert.io> - 0.2.0-1
- Packaged release
