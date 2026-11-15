# TODO

- Run the project's own check command when the model says it is done, send a failure back as the next
  message, do it once per answer, and accept it only from a trusted project.
- Run the configured formatter on the files an edit touched, and say so in the result.
- Bring back a loop that runs the same tasks and counts passes, seconds and prompt tokens: the
  decisions below need measurement, and the loop that did this is gone.
- Decide whether older thinking earns the tokens it costs on every request before the setting that
  sends it back is turned on by default.
- Name the room held back for the answer in the message that says a conversation no longer fits: it
  reads as though the window were spare, when what ran out is that room.
- Stop the thinking budget collapsing to the same size at every level when the window is small.
- Write a turn's lines to the session file at once, or hold it open, instead of opening and closing it
  for every saved message.
