.PHONY: run-example
run-example:
	@echo "Creating example environment..."
	docker-compose -f example/docker-compose.yml up --build