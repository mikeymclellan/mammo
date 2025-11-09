package model

type Account struct {
    ID    string
    Name  string
    Email string
}

func NewAccount(id, name, email string) *Account {
    return &Account{
        ID:    id,
        Name:  name,
        Email: email,
    }
}

func (a *Account) GetAccountInfo() map[string]string {
    return map[string]string{
        "id":    a.ID,
        "name":  a.Name,
        "email": a.Email,
    }
}

