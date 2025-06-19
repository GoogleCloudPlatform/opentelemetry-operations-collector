FROM gcr.io/cloud-builders/git
ARG CUSTOM_COMPONENTS
RUN echo ${CUSTOM_COMPONENTS}
RUN git clone ${CUSTOM_COMPONENTS} /test
WORKDIR /test
RUN ls -lra
