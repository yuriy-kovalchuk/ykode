# TODO

- **A container launcher** (S–M) — a script that starts ykode in a container with one chosen
  directory mounted as the project, so a model-run command reaches that volume and nothing else of
  `$HOME`. To decide: the image (built from the repo or a released binary), how `base_url` reaches
  the host's server (`host.docker.internal`, or host networking on Linux), whether `$YKODE_HOME`
  is mounted so sessions survive, and whether the network beyond the server is cut.
