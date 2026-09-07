// Runs from stdin. No message content is placed in process arguments or files.
function run() {
    const app = Application('Mail');
    const q = request;
    function accounts() {
        const result = [];
        if (app.accounts().some(a => a.enabled() && !a.emailAddresses().length)) {
            const blank = app.OutgoingMessage({visible:false,subject:'',content:''});
            app.outgoingMessages.push(blank);
            blank.sender();
            blank.close({saving:'no'});
        }
        app.accounts().forEach(a => {
            if (!a.enabled()) return;
            let addresses = a.emailAddresses();
            addresses.filter(address => address.includes('@')).forEach(address => result.push({accountId:a.id(), name:a.name(), address:address}));
        });
        return result;
    }
    function target() {
        const acc = app.accounts.byId(q.resolvedAccount);
        const msg = acc.mailboxes.byName('INBOX').messages.byId(q.localId);
        if (!msg.exists() || msg.deletedStatus()) throw Error('missing message');
        return msg;
    }
    function addresses(collection) { return collection().map(r => r.address()); }
    if (q.op === 'accounts') return JSON.stringify({ok:true,accounts:accounts()});
    if (q.op === 'read') {
        const m = target();
        if (m.messageSize() > 32*1024*1024) return JSON.stringify({ok:false,error:'Message exceeds the 32 MiB reading limit.'});
        return JSON.stringify({ok:true,message:{id:q.id,accountId:q.resolvedAccount,
            subject:m.subject(),sender:m.sender(),replyTo:m.replyTo(),date:m.dateReceived().toISOString(),
            to:addresses(m.toRecipients),cc:addresses(m.ccRecipients),read:m.readStatus(),
            body:m.content() || '',source:m.source() || '',attachments:m.mailAttachments().map(a=>a.name())}});
    }
    if (q.op === 'mark') { target().readStatus = q.read; return JSON.stringify({ok:true}); }
    throw Error('unsupported operation');
}
