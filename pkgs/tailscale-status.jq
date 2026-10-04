{
  state: (.BackendState // ""),
  authURL: (.AuthURL // ""),
  dnsName: ((.Self.DNSName // "") | rtrimstr(".")),
  owner: (
    if (.Self.UserID // 0) == 0 then ""
    else ((.User // {})[(.Self.UserID | tostring)].LoginName // "")
    end
  )
}
