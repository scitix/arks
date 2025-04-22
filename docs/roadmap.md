# ARKS Project Roadmap
> "The future belongs to those who believe in the beauty of their dreams." — Eleanor Roosevelt

## 1. Unified Model Management
Optimize compute efficiency and accelerate inference performance.

- `Multi-source model downloads` – Support fetching models from diverse repositories.
- `Model preloading` – Reduce latency by preloading frequently used models.
- `Near-compute-node acceleration` – Improve the performance of loading models.

## 2. LLM Application Deployment
One-click deployment for simplicity and speed.
- `Multi-runtime support` – Compatible with Dynamo, vLLM, and SGLang.
- `Distributed inference` –
    - Multi-node inference (LeaderWorkerSet)
    - PD-separated inference (Dynamo)
- `Heterogeneous inference service deployment` – Run across different hardware types.
- `Extended parameter support` – Flexible runtime optimization.
- `SLO-based autoscaling` – Dynamically adjust resources based on service-level objectives.

## 3. Gateway Features
Intelligent routing and multi-tenant control.

- `Multi-model load balancing` – Efficiently distribute inference requests.
- `Model service discovery` – Automatically detect and route to available instances.
- `Multi-tenant management` –
    - Model-level access control
    - Model-level quota management
    - Model-level request rate limiting
- `Tenant domain & certificate management` – Secure and isolate tenant traffic.
- `External SLB support` – Integrate with external load balancers.

## 4. Observability & Monitoring
- `Prometheus integration` – Collect and visualize key metrics.

## 5. Management Console
Centralized control for administrators.
- `Multi-tenant management` – Assign roles, permissions, and resources.
- `User management` – Token-based authentication & quota enforcement.
- `Visual dashboard` – Monitor system health and performance at a glance.

This roadmap outlines our vision for ARKS—a scalable, efficient, and user-friendly platform for AI model deployment and management. 🚀
