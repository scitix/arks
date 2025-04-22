# Model Usage

## Overview
ArksModel is a solution designed to optimize the LLM model management workflow. It prepares models before GPU instance initialization and shares them with LLM application instances via high-performance shared storage. This significantly improves model loading efficiency during LLM application startup, thereby optimizing GPU resource utilization. The working principle is illustrated in the following diagram:

![model-architecture-v1](docs/images/model-architecture-v1.png)