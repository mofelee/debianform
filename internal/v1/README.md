# Legacy v1 Implementation

本目录包含 v1 parser、executor、providers、SSH runner 和远端 state backend。

它在 v2 设计和实现期间作为参考代码保留。新的 v2 代码不应该继续扩展 v1 用户模型，
也不应该把 `debian_*` 资源作为主要 DSL 暴露给用户。

边界清晰的实现片段可以在后续复制或改造进新的 v2 包。
