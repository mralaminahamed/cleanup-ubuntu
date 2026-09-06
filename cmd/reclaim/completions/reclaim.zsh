#compdef reclaim
# zsh completion for reclaim

_reclaim_units() {
    local -a ids
    ids=(${(f)"$(reclaim __units 2>/dev/null)"})
    _describe 'unit' ids
}

_reclaim() {
    local -a cmds clean_flags groups
    cmds=(
        'clean:report reclaimable space, or reclaim it with --apply'
        'status:show filesystems and disk pressure'
        'analyze:list the largest directories, deleting nothing'
        'history:show what past runs deleted'
        'version:print the version'
        'completion:print a shell completion script'
    )
    groups=(gradle maven jetbrains browsers playwright docker docker-volumes
            claude-vm system claude-jobs claude-plugins claude-history
            heavy flatpak kernels models)
    clean_flags=(
        '--apply[actually delete]'
        '--yes[do not prompt]'
        '--free[clean until SIZE is free]:size'
        '--auto[pick a target from disk pressure]'
        '--below[do nothing unless free space is under SIZE]:size'
        '--tier[highest tier to run]:tier'
        '--allow-lossy[permit units that destroy information]'
        '--discover[claim caches with no hardcoded rule]'
        '--json[machine-readable output]'
        '--workers[parallel probe workers]:count'
        '--sites-idle[include trees of projects idle N days]:days'
        '--sites-root[where projects live]:directory:_files -/'
        '--only[restrict to these unit ids]:unit:_reclaim_units'
        '--exclude[drop these unit ids]:unit:_reclaim_units'
        '--with[opt-in flag by name]:group:(gradle maven jetbrains browsers playwright docker docker-volumes claude-vm system claude-jobs claude-plugins claude-history heavy flatpak kernels models)'
        '--gradle[opt in to gradle units]'
        '--maven[opt in to maven units]'
        '--jetbrains[opt in to jetbrains units]'
        '--browsers[opt in to browser units]'
        '--playwright[opt in to playwright units]'
        '--docker[opt in to docker units]'
        '--docker-volumes[opt in to docker volume units]'
        '--claude-vm[opt in to Claude VM units]'
        '--system[opt in to system units]'
        '--claude-jobs[opt in to Claude job units]'
        '--claude-plugins[opt in to Claude plugin units]'
        '--claude-history[opt in to Claude transcript units]'
        '--heavy[opt in to large discovered caches]'
        '--flatpak[opt in to flatpak units]'
        '--kernels[opt in to superseded kernels]'
        '--models[opt in to model stores]'
    )

    if (( CURRENT == 2 )); then
        _describe 'command' cmds
        return
    fi

    case "${words[2]}" in
        clean)      _arguments $clean_flags ;;
        analyze)    _arguments '--min[only report dirs at least this large]:size' '-n[how many entries]:count' ;;
        history)    _arguments '-n[how many entries]:count' ;;
        completion) _values 'shell' bash zsh fish ;;
    esac
}

_reclaim "$@"
