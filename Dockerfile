# GoReleaser builds the binaries and passes them in per platform.
# distroless/static has CA certificates for alive2.llvm.org and runs as nonroot.
FROM gcr.io/distroless/static-debian12:nonroot
ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/alive2-mcp /usr/bin/alive2-mcp
# Lets the MCP Registry verify that this image belongs to the listed server.
LABEL io.modelcontextprotocol.server.name="io.github.phongndo/alive2-mcp"
ENTRYPOINT ["/usr/bin/alive2-mcp"]
