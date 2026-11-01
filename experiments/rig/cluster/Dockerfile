# The job image for the cluster experiments: the three binaries, built on the host as static files and
# copied in. Building inside the image would download the module cache on every run — minutes for a
# measurement that takes seconds.
FROM scratch
COPY bin/ckpttrainer /ckpttrainer
COPY bin/ckptctl /ckptctl
COPY bin/ckpt-observer /ckpt-observer
ENTRYPOINT ["/ckpttrainer"]
