FROM golang:alpine as BUILD

WORKDIR /serialToTcpBridge

COPY ./ ./

RUN ls

RUN go build

FROM alpine:latest as RUN

WORKDIR /serialToTcpBridge

COPY --from=BUILD /serialToTcpBridge/SerialToTCPBridgeProtocol ./SerialToTCPBridgeProtocol

CMD ["./SerialToTCPBridgeProtocol"]