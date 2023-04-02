#include "stdio.h"

int main()
{
    printf("%s\n", "abc");

    int i = 1;
    int *p = &i;

    printf("%d", *p);

    return 0;
}