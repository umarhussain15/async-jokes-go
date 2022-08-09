
# STEP 1: use golang image to build an executable which will be used in container
FROM golang AS builder

# Set current directory in container to copy from host
WORKDIR $GOPATH/src/

# copy files from given host directory to current folder
COPY . .

# only dowload dependencies of go required for code.
RUN go get -d -v

# build current folder and store executable file.
RUN CGO_ENABLED=0 GOOS=linux go build -o /go/bin/jokes-app

# STEP 2: build container with executable with scratch as base.
FROM scratch

# Copy our static executable.
COPY --from=builder /go/bin/jokes-app /go/bin/jokes-app

# set executable as entry point during container startup.
CMD ["/go/bin/jokes-app"]
ENTRYPOINT ["/go/bin/jokes-app"]