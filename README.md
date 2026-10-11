# Homie

Dashboard per homelab: tutti i servizi in un unico posto, organizzati in gruppi e gestiti interamente dall'interfaccia web. Aggiungi, modifichi, elimini e riordini servizi e gruppi trascinandoli, senza toccare file di configurazione.

Pensata per girare in una LXC su Proxmox VE e per essere usata solo in rete locale: non ha login.

## Installazione su Proxmox

Dalla shell dell'host Proxmox:

```bash
bash -c "$(curl -fsSL https://raw.githubusercontent.com/Bagareal/homie/main/install.sh)"
```

Lo script crea una LXC Debian non privilegiata (predefiniti: 1 core, 512 MB di RAM, 4 GB di disco, DHCP su vmbr0, avvio automatico), installa l'ultima release verificandone il checksum e avvia il servizio. Alla fine stampa l'indirizzo della dashboard, raggiungibile sulla porta 8080.

Rispondendo "n" alla prima domanda puoi scegliere ID, hostname, risorse, storage, bridge e IP statico.

## Aggiornamento

Dall'host Proxmox:

```bash
pct exec <ID> -- homie-update
```

oppure `homie-update` dalla console della LXC. Scarica l'ultima release e riavvia il servizio; la configurazione non viene toccata.

## Dove stanno i dati

| Percorso | Contenuto |
| --- | --- |
| `/var/lib/homie/config.json` | Gruppi, servizi e impostazioni |
| `/var/lib/homie/icons/` | Icone locali, raggiungibili come `/icons/nome.png` |
| `/usr/local/bin/homie` | Il programma |
| `/etc/systemd/system/homie.service` | Il servizio (utente di sistema `homie`) |

La configurazione si può anche esportare e importare dalla pagina Impostazioni. Il backup della LXC con PBS la include.

## Icone

Nel campo Icona di un servizio puoi scrivere:

- il nome di un'icona della raccolta [dashboard-icons](https://github.com/homarr-labs/dashboard-icons), per esempio `proxmox`, `truenas-scale`, `adguard-home`, `jellyfin`
- l'indirizzo completo di un'immagine
- un file messo in `/var/lib/homie/icons/`, per esempio `/icons/router.png`
- un emoji o un carattere

Se il campo è vuoto o l'icona non si carica, il riquadro mostra le iniziali del servizio.

## Stato dei servizi

Il backend controlla periodicamente ogni servizio con il controllo attivo (intervallo nelle Impostazioni). Un servizio è online se risponde con un codice HTTP inferiore a 500: anche una pagina di login (401, 403 o un redirect) conta come online, mentre un 502 o 503 di un reverse proxy senza backend no. I certificati self-signed sono accettati.

## Aspetto

Dalla sezione Aspetto in Impostazioni si personalizzano tema (chiaro/scuro/automatico), densità delle tile, e tre colori: sfondo della pagina, sfondo delle tile e colore d'accento (usato per i titoli dei gruppi e gli indicatori di trascinamento). Ogni colore ha un pulsante "Reset" per tornare al valore predefinito del tema.

## Struttura del progetto

```
cmd/homie/              avvio del programma
internal/config/        modello dei dati, validazione, salvataggio
internal/status/        controllo periodico dei servizi
internal/server/        API HTTP e pagine
internal/webui/         interfaccia compilata, incorporata nel binario
ui/                     interfaccia (Svelte 5, TypeScript, Vite)
scripts/setup.sh        installazione e aggiornamento dentro la LXC
install.sh              installer per l'host Proxmox
```

Il risultato della build è un unico eseguibile senza dipendenze, per amd64 e arm64.

## Sviluppo

Servono Go 1.22 o successivo e Node.js 22.

```bash
make dev     # backend su :8080, interfaccia con ricaricamento automatico su :5173
make build   # compila interfaccia e binario in bin/homie
make run     # compila e avvia con i dati in ./data
make test    # esegue i test Go
```

## Pubblicare una versione

```bash
git tag v0.1.0
git push origin v0.1.0
```

GitHub Actions compila interfaccia e binari e crea la release con i file `homie-linux-amd64`, `homie-linux-arm64` e `SHA256SUMS`, che sono quelli scaricati da installer e aggiornamento.

## API

| Metodo | Percorso | Descrizione |
| --- | --- | --- |
| GET | `/api/config` | Configurazione completa |
| PUT | `/api/config` | Sostituisce la configurazione (validata) |
| GET | `/api/status` | Ultimo stato di ogni servizio controllato |
| GET | `/api/version` | Versione in esecuzione |
