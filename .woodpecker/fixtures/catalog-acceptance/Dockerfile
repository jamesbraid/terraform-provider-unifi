FROM scratch
COPY unifi-emu /unifi-emu
COPY devices.fleet.json /devices.json
ENV SIM_DEFAULT_DEVICES=/devices.json
HEALTHCHECK --interval=2s --timeout=2s --start-period=5s --retries=3 \
    CMD ["/unifi-emu", "-healthcheck"]
ENTRYPOINT ["/unifi-emu"]
