; Shared Hosts outlive desktop windows. Never use the default process-killing
; upgrade/uninstall path. Conservatively block while any named companion runs,
; including companions outside this installation; users stop their own work.
!macro customCheckAppRunning
  ${nsProcess::FindProcess} "channelterm-desktop.exe" $R0
  ${If} $R0 != 603
    IfSilent +2
    MessageBox MB_OK|MB_ICONEXCLAMATION "Close ChannelTerm Desktop before installing or removing it. No process was stopped."
    SetErrorLevel 2
    Quit
  ${EndIf}
  ${nsProcess::FindProcess} "channelterm.exe" $R0
  ${If} $R0 != 603
    IfSilent +2
    MessageBox MB_OK|MB_ICONEXCLAMATION "A ChannelTerm Host or CLI may still be running. Finish shared Session work and stop it explicitly before installing or removing the desktop package. No process was stopped."
    SetErrorLevel 2
    Quit
  ${EndIf}
!macroend
