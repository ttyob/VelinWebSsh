# Velin Web SSH for fnOS

This directory is a fnOS Docker application package source. The package runs
the published Velin image and the guacd dependency as a Docker project.

Build it from the repository root:

```sh
packaging/fnos/build.sh
```

Use a release tag or image override when needed:

```sh
VELIN_FNOS_VERSION=0.3.17 packaging/fnos/build.sh
VELIN_FNOS_IMAGE=ghcr.io/ttyob/velinwebssh:latest packaging/fnos/build.sh
```

Install the resulting `.fpk` through fnOS App Center for local testing. The
package needs Docker support enabled on the NAS and outbound access to pull
the Velin, guacd, and Alpine images.
