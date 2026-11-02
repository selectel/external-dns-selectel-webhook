FROM gcr.io/distroless/static-debian13:nonroot

COPY external-dns-selectel-webhook /external-dns-selectel-webhook

ENTRYPOINT ["/external-dns-selectel-webhook"]
