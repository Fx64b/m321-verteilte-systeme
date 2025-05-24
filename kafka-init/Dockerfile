FROM confluentinc/cp-kafka:latest

USER root
RUN microdnf install -y dos2unix

COPY create-topics.sh /create-topics.sh
RUN dos2unix /create-topics.sh && chmod +x /create-topics.sh

USER appuser
CMD ["/create-topics.sh"]