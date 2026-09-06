# fish completion for reclaim

function __reclaim_units
    reclaim __units 2>/dev/null
end

set -l cmds clean status analyze history version completion
complete -c reclaim -f

complete -c reclaim -n "not __fish_seen_subcommand_from $cmds" -a clean -d 'report reclaimable space'
complete -c reclaim -n "not __fish_seen_subcommand_from $cmds" -a status -d 'filesystems and disk pressure'
complete -c reclaim -n "not __fish_seen_subcommand_from $cmds" -a analyze -d 'largest directories'
complete -c reclaim -n "not __fish_seen_subcommand_from $cmds" -a history -d 'what past runs deleted'
complete -c reclaim -n "not __fish_seen_subcommand_from $cmds" -a version -d 'print the version'
complete -c reclaim -n "not __fish_seen_subcommand_from $cmds" -a completion -d 'shell completion script'

complete -c reclaim -n '__fish_seen_subcommand_from completion' -a 'bash zsh fish'

complete -c reclaim -n '__fish_seen_subcommand_from clean' -l apply -d 'actually delete'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l yes -d 'do not prompt'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l free -r -d 'clean until SIZE is free'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l auto -d 'target from disk pressure'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l below -r -d 'act only under SIZE free'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l tier -r -d 'highest tier to run'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l allow-lossy -d 'permit lossy units'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l discover -d 'claim unlisted caches'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l json -d 'machine-readable output'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l workers -r -d 'probe workers'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l sites-idle -r -d 'projects idle N days'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l sites-root -r -d 'where projects live'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l only -r -a '(__reclaim_units)' -d 'restrict to unit ids'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l exclude -r -a '(__reclaim_units)' -d 'drop unit ids'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l with -r -d 'opt-in flag by name'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l gradle -d 'opt in to gradle units'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l maven -d 'opt in to maven units'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l jetbrains -d 'opt in to jetbrains units'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l browsers -d 'opt in to browser units'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l playwright -d 'opt in to playwright units'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l docker -d 'opt in to docker units'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l docker-volumes -d 'opt in to docker volumes'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l claude-vm -d 'opt in to Claude VM units'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l system -d 'opt in to system units'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l claude-jobs -d 'opt in to Claude job units'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l claude-plugins -d 'opt in to Claude plugin cache'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l claude-history -d 'opt in to Claude transcripts'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l heavy -d 'opt in to large discovered caches'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l flatpak -d 'opt in to flatpak units'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l kernels -d 'opt in to superseded kernels'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l models -d 'opt in to model stores'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l xcode -d 'opt in to Xcode state'
complete -c reclaim -n '__fish_seen_subcommand_from clean' -l simulators -d 'opt in to simulator devices'

complete -c reclaim -n '__fish_seen_subcommand_from analyze' -l min -r -d 'minimum size to report'
complete -c reclaim -n '__fish_seen_subcommand_from analyze' -l installers -d 'also report stale downloads'
complete -c reclaim -n '__fish_seen_subcommand_from analyze' -l older -r -d 'days before an installer is stale'
complete -c reclaim -n '__fish_seen_subcommand_from analyze' -l json -d 'machine-readable output'
complete -c reclaim -n '__fish_seen_subcommand_from analyze' -s n -r -d 'how many entries'
complete -c reclaim -n '__fish_seen_subcommand_from history' -s n -r -d 'how many entries'
