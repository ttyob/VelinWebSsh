# Velin Web SSH for fnOS

This directory is a native fnOS application package source. The package runs
the Velin Go binary and guacd as native processes. Docker is not required on
the NAS.

Build it from the repository root:

```sh
packaging/fnos/build.sh
```

Build a package for the current host architecture:

```sh
VELIN_FNOS_VERSION=0.3.19 packaging/fnos/build.sh
```

The release workflow builds separate `amd64` and `arm64` packages. The build
machine needs Docker to extract the matching guacd runtime files, but Docker is
not needed after installation. Install the matching `.fpk` through fnOS App
Center. Crush is optional; set `VELIN_FNOS_INCLUDE_CRUSH=1` when the AI Agent
is needed. ffmpeg is optional and uses the NAS `ffmpeg` command when
recordings are enabled.
