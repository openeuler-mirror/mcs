# 术语表

- MICA：本仓讨论的混合部署系统
- master side：Linux 侧控制与编排域
- client side：RTOS 侧运行域
- pedestal：用于适配不同运行环境的承载层/适配层
- OpenAMP：远程处理器通信与控制的底层框架
- libmetal：OpenAMP 及相关运行时依赖的平台抽象层
- RPMsg：基于共享内存传输的消息通信机制
- remoteproc：远程处理器生命周期管理模型
- MicRun：本仓内建立在 MICA 之上的独立子项目；实时容器、容器编排、RTOS容器、混部容器在本仓语境下统一指向该子项目
